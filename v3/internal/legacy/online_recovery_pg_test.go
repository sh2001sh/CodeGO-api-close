//go:build pgintegration

package legacy

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func onlineRecoveryExec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

func onlineRecoveryTotals(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var raw []byte
	if err := pool.QueryRow(context.Background(), `SELECT COALESCE(jsonb_object_agg(name,value),'{}') FROM v3_migration_online.totals`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func onlineRecoveryAssertNoMoney(t *testing.T, target *pgxpool.Pool) {
	t.Helper()
	var count int64
	if err := target.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM v3_billing.accounts)+(SELECT count(*) FROM v3_billing.ledger_entries)+(SELECT count(*) FROM v3_billing.balance_outbox)`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("online recovery posted live money count=%d err=%v", count, err)
	}
}

func onlineRecoveryStageUsage(t *testing.T, target *pgxpool.Pool, id int64, request string, seconds int64) {
	t.Helper()
	var actual string
	var created time.Time
	if err := target.QueryRow(context.Background(), "SELECT request_id,created_at FROM "+onlineStage("v3_billing.usage_logs")+" WHERE id=$1", id).Scan(&actual, &created); err != nil || actual != request || created.Unix() != seconds {
		t.Fatalf("usage id=%d request=%q stamp=%d expected=%q/%d err=%v", id, actual, created.Unix(), request, seconds, err)
	}
}

type onlineRecoveryCancelTracer struct {
	cancel context.CancelFunc
	fired  atomic.Bool
}

func (tracer *onlineRecoveryCancelTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(data.SQL, "SELECT to_jsonb(t)") && strings.Contains(data.SQL, `"migration_source"."logs"`) && strings.Contains(data.SQL, "WHERE ROW(") && tracer.fired.CompareAndSwap(false, true) {
		tracer.cancel()
	}
	return ctx
}
func (*onlineRecoveryCancelTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestOnlineRecoveryCopyCheckpointAfterBadBatchOrCancellation(t *testing.T) {
	for _, failure := range []string{"bad_second_batch", "cancel_after_first_commit"} {
		t.Run(failure, func(t *testing.T) {
			m, source, target, opts := onlineMigrationFixture(t, false)
			ctx := context.Background()
			onlineRecoveryExec(t, source, `INSERT INTO migration_source.logs SELECT 10000+g,7,1700010000+g,2,'recovery','alice','key','chat-model',1,1,1,0,false,13,11,'default','','recovery-'||g,'','{}' FROM generate_series(1,1000) g`)
			if failure == "bad_second_batch" {
				onlineRecoveryExec(t, source, `UPDATE migration_source.logs SET quota=-1 WHERE id=10600`)
			}
			if _, err := m.PrepareOnline(ctx, opts, true); err != nil {
				t.Fatal(err)
			}
			copyImporter := m
			copyCtx := ctx
			var tracer *onlineRecoveryCancelTracer
			if failure == "cancel_after_first_commit" {
				var cancel context.CancelFunc
				copyCtx, cancel = context.WithCancel(ctx)
				defer cancel()
				tracer = &onlineRecoveryCancelTracer{cancel: cancel}
				config := source.Config().Copy()
				config.ConnConfig.Tracer = tracer
				tracedSource, err := pgxpool.NewWithConfig(ctx, config)
				if err != nil {
					t.Fatal(err)
				}
				defer tracedSource.Close()
				clone := *m
				clone.source = tracedSource
				copyImporter = &clone
			}
			if _, err := copyImporter.CopyOnline(copyCtx, opts); err == nil {
				t.Fatal("injected second-batch interruption was accepted")
			}
			if tracer != nil && !tracer.fired.Load() {
				t.Fatal("cancel injection did not reach the second batch")
			}
			var copied, rows int64
			var complete bool
			if err := target.QueryRow(ctx, `SELECT copied,complete FROM v3_migration_online.progress WHERE name='logs'`).Scan(&copied, &complete); err != nil || copied != 512 || complete {
				t.Fatalf("copy checkpoint copied=%d complete=%t err=%v", copied, complete, err)
			}
			if err := target.QueryRow(ctx, "SELECT count(*) FROM "+onlineStage("v3_audit.events")).Scan(&rows); err != nil || rows != 512 {
				t.Fatalf("bad/canceled batch partially committed rows=%d err=%v", rows, err)
			}
			onlineRecoveryAssertNoMoney(t, target)
			if failure == "bad_second_batch" {
				onlineRecoveryExec(t, source, `UPDATE migration_source.logs SET quota=1 WHERE id=10600`)
			}
			if r, err := m.CopyOnline(ctx, opts); err != nil || r.Phase != "copied" {
				t.Fatalf("resumed copy %+v err=%v", r, err)
			}
			onlineMigrationSync(t, m, opts)
			if _, err := m.VerifyOnline(ctx, opts); err != nil {
				t.Fatal(err)
			}
			if err := target.QueryRow(ctx, `SELECT copied FROM v3_migration_online.progress WHERE name='logs'`).Scan(&copied); err != nil || copied != 1004 {
				t.Fatalf("resumed checkpoint duplicated first batch copied=%d err=%v", copied, err)
			}
			if err := target.QueryRow(ctx, "SELECT count(*) FROM "+onlineStage("v3_audit.events")).Scan(&rows); err != nil || rows != 1004 {
				t.Fatalf("resumed baseline lost/duplicated rows=%d err=%v", rows, err)
			}
		})
	}
}

func TestOnlineRecoverySyncLateLowIDCommit(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	onlineMigrationReady(t, m, opts)
	low, err := source.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = low.Rollback(ctx) }()
	if _, err = low.Exec(ctx, `UPDATE migration_source.logs SET quota=55 WHERE id=51`); err != nil {
		t.Fatal(err)
	}
	var lowID, highID int64
	if err = low.QueryRow(ctx, `SELECT id FROM v3_migration_capture.events WHERE table_name='migration_source.logs' AND row_key='{"id":51}'`).Scan(&lowID); err != nil {
		t.Fatal(err)
	}
	onlineRecoveryExec(t, source, `UPDATE migration_source.logs SET content='higher-ID committed first' WHERE id=52`)
	if err = source.QueryRow(ctx, `SELECT id FROM v3_migration_capture.events WHERE table_name='migration_source.logs' AND row_key='{"id":52}'`).Scan(&highID); err != nil || highID <= lowID {
		t.Fatalf("late commit fixture did not allocate ordered IDs low=%d high=%d err=%v", lowID, highID, err)
	}
	if r, err := m.SyncOnline(ctx, opts); err != nil || r.Acknowledged != 1 || r.Pending != 0 {
		t.Fatalf("sync with low-ID transaction in flight %+v err=%v", r, err)
	}
	if err = low.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if r, err := m.SyncOnline(ctx, opts); err != nil || r.Acknowledged != 1 || r.Pending != 0 {
		t.Fatalf("late low-ID commit lost %+v err=%v", r, err)
	}
	var amount int64
	if err = target.QueryRow(ctx, "SELECT amount FROM "+onlineStage("v3_audit.events")+" WHERE id=51").Scan(&amount); err != nil || amount != 110 {
		t.Fatalf("late low-ID state missing amount=%d err=%v", amount, err)
	}
	if _, err = m.VerifyOnline(ctx, opts); err != nil {
		t.Fatal(err)
	}
	onlineRecoveryAssertNoMoney(t, target)
}

func TestOnlineRecoveryCommittedTargetFailedAckReplaysExactly(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	onlineMigrationReady(t, m, opts)
	onlineRecoveryExec(t, source, `UPDATE migration_source.logs SET quota=55 WHERE id=51`)
	bad := opts
	bad.SourceAdmin = readonlySource(t, source)
	// Permit identity verification while retaining the intended ACK denial.
	onlineRecoveryExec(t, source, "GRANT EXECUTE ON FUNCTION pg_catalog.pg_control_system() TO "+pgx.Identifier{bad.SourceAdmin.Config().ConnConfig.User}.Sanitize())
	if _, err := m.SyncOnline(ctx, bad); err == nil {
		t.Fatal("source ack unexpectedly succeeded through the read-only role")
	}
	var amount int64
	if err := target.QueryRow(ctx, "SELECT amount FROM "+onlineStage("v3_audit.events")+" WHERE id=51").Scan(&amount); err != nil || amount != 110 {
		t.Fatalf("target did not commit before failed ack amount=%d err=%v", amount, err)
	}
	var pending int64
	if err := source.QueryRow(ctx, `SELECT count(*) FROM v3_migration_capture.events WHERE NOT acked`).Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("failed ack discarded retry evidence pending=%d err=%v", pending, err)
	}
	before := onlineRecoveryTotals(t, target)
	onlineMigrationSync(t, m, opts)
	onlineMigrationSync(t, m, opts)
	if after := onlineRecoveryTotals(t, target); after != before {
		t.Fatalf("target replay duplicated financial/count totals before=%s after=%s", before, after)
	}
	if _, err := m.VerifyOnline(ctx, opts); err != nil {
		t.Fatal(err)
	}
	onlineRecoveryAssertNoMoney(t, target)
}

func TestOnlineRecoveryLogDuplicateGroupsAndPartitionKeyMoves(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	onlineRecoveryExec(t, source, `DELETE FROM migration_source.logs WHERE id=54`)
	onlineMigrationReady(t, m, opts)
	onlineRecoveryStageUsage(t, target, 51, "kept-request", 1700000000)
	onlineRecoveryExec(t, source, `INSERT INTO migration_source.logs SELECT x.* FROM migration_source.logs l CROSS JOIN LATERAL jsonb_populate_record(NULL::migration_source.logs,to_jsonb(l)||'{"id":54,"quota":10,"content":"duplicate"}'::jsonb)x WHERE l.id=51`)
	onlineMigrationSync(t, m, opts)
	onlineRecoveryStageUsage(t, target, 51, "kept-request:v2-log:51", 1700000000)
	onlineRecoveryStageUsage(t, target, 54, "kept-request:v2-log:54", 1700000000)
	// Move the composite partition key into a different month. The previous
	// (created_at,id) must be removed, and the old/new groups both normalized.
	onlineRecoveryExec(t, source, `UPDATE migration_source.logs SET created_at=1735689600 WHERE id=54`)
	onlineMigrationSync(t, m, opts)
	onlineRecoveryStageUsage(t, target, 51, "kept-request", 1700000000)
	onlineRecoveryStageUsage(t, target, 54, "kept-request", 1735689600)
	var count int64
	if err := target.QueryRow(ctx, "SELECT count(*) FROM "+onlineStage("v3_billing.usage_logs")+" WHERE id=54").Scan(&count); err != nil || count != 1 {
		t.Fatalf("old partition-key row survived move count=%d err=%v", count, err)
	}
	onlineRecoveryExec(t, source, `UPDATE migration_source.logs SET created_at=1700000000 WHERE id=54`)
	onlineMigrationSync(t, m, opts)
	onlineRecoveryStageUsage(t, target, 51, "kept-request:v2-log:51", 1700000000)
	onlineRecoveryExec(t, source, `DELETE FROM migration_source.logs WHERE id=54`)
	onlineMigrationSync(t, m, opts)
	onlineRecoveryStageUsage(t, target, 51, "kept-request", 1700000000)
	var original string
	if err := target.QueryRow(ctx, "SELECT request_id FROM "+onlineStage("v3_audit.events")+" WHERE id=51").Scan(&original); err != nil || original != "kept-request" {
		t.Fatalf("audit evidence request ID was rewritten original=%q err=%v", original, err)
	}
	if _, err := m.VerifyOnline(ctx, opts); err != nil {
		t.Fatal(err)
	}
}

func TestOnlineRecoveryParentOnlyDeleteAndReinsertReclassifiesAttempts(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	onlineMigrationReady(t, m, opts)
	var parent []byte
	if err := source.QueryRow(ctx, `SELECT to_jsonb(r) FROM gateway.request_audits r WHERE request_id='kept-request'`).Scan(&parent); err != nil {
		t.Fatal(err)
	}
	onlineRecoveryExec(t, source, `DELETE FROM gateway.request_audits WHERE request_id='kept-request'`)
	onlineMigrationSync(t, m, opts)
	var linked, orphan int64
	if err := target.QueryRow(ctx, "SELECT (SELECT count(*) FROM "+onlineStage("v3_audit.request_attempt_audits")+" WHERE attempt_id='kept-attempt'),(SELECT count(*) FROM "+onlineStage("v3_audit.orphan_request_attempt_history")+" WHERE attempt_id='kept-attempt')").Scan(&linked, &orphan); err != nil || linked != 0 || orphan != 1 {
		t.Fatalf("parent-only delete linked=%d orphan=%d err=%v", linked, orphan, err)
	}
	onlineRecoveryExec(t, source, `INSERT INTO gateway.request_audits SELECT * FROM jsonb_populate_record(NULL::gateway.request_audits,$1::jsonb)`, parent)
	onlineMigrationSync(t, m, opts)
	if err := target.QueryRow(ctx, "SELECT (SELECT count(*) FROM "+onlineStage("v3_audit.request_attempt_audits")+" WHERE attempt_id='kept-attempt'),(SELECT count(*) FROM "+onlineStage("v3_audit.orphan_request_attempt_history")+" WHERE attempt_id='kept-attempt')").Scan(&linked, &orphan); err != nil || linked != 1 || orphan != 0 {
		t.Fatalf("parent-only reinsert linked=%d orphan=%d err=%v", linked, orphan, err)
	}
	if _, err := m.VerifyOnline(ctx, opts); err != nil {
		t.Fatal(err)
	}
}

func TestOnlineRecoveryStagingGuardAndIndependentVerifyRejectExtraRow(t *testing.T) {
	m, _, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	onlineMigrationReady(t, m, opts)
	stage := onlineStage("v3_audit.events")
	for _, sql := range []string{"UPDATE " + stage + " SET amount=0 WHERE id=51", "DELETE FROM " + stage + " WHERE id=51", "TRUNCATE " + stage} {
		if _, err := target.Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "migration ownership") {
			t.Fatalf("unowned staging mutation allowed sql=%s err=%v", sql, err)
		}
	}
	if err := pgx.BeginFunc(ctx, target, func(tx pgx.Tx) error {
		if err := onlineAuthorize(ctx, tx, opts.RunID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "INSERT INTO "+stage+" SELECT x.* FROM "+stage+" l CROSS JOIN LATERAL jsonb_populate_record(NULL::"+stage+",to_jsonb(l)||'{\"id\":999999}'::jsonb)x WHERE l.id=51")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.VerifyOnline(ctx, opts); err == nil {
		t.Fatal("independent verification accepted an unexpected target row")
	}
	onlineRecoveryAssertNoMoney(t, target)
}

func TestOnlineRecoveryFinalRejectsStructuralSourceDrift(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	onlineMigrationReady(t, m, opts)
	onlineRecoveryExec(t, source, `UPDATE billing.accounts SET owner_id=99 WHERE account_id='wallet-7'`)
	if _, err := SealOnlineCapture(ctx, source, opts.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.FinalizeOnline(ctx, opts); err == nil || !strings.Contains(err.Error(), "structural dependency") {
		t.Fatalf("final accepted changed account ownership err=%v", err)
	}
	onlineRecoveryAssertNoMoney(t, target)
	var sealed bool
	if err := source.QueryRow(ctx, `SELECT sealed FROM v3_migration_capture.config`).Scan(&sealed); err != nil || !sealed {
		t.Fatalf("failed final implicitly reopened source sealed=%t err=%v", sealed, err)
	}
}

func TestOnlineRecoveryCaptureBindingRejectsSecondTarget(t *testing.T) {
	m, source, _, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	onlineMigrationReady(t, m, opts)
	var binding string
	if err := source.QueryRow(ctx, `SELECT COALESCE(target_identity,'') FROM v3_migration_capture.config`).Scan(&binding); err != nil || binding == "" {
		t.Fatalf("prepare did not permanently bind its target binding=%q err=%v", binding, err)
	}
	if err := BindOnlineCaptureTarget(ctx, source, opts.RunID, binding); err != nil {
		t.Fatal(err)
	}
	different := strings.Repeat("f", 64)
	if different == binding {
		different = strings.Repeat("e", 64)
	}
	if err := BindOnlineCaptureTarget(ctx, source, opts.RunID, different); err == nil {
		t.Fatal("capture can be rebound to another target")
	}
	_, target2, _ := importTestDB(t)
	other := NewImporter(source, target2, m.crypto)
	if _, err := other.PrepareOnline(ctx, opts, true); err == nil {
		t.Fatal("second target reused the acknowledged capture stream")
	}
	var count int64
	if err := target2.QueryRow(ctx, `SELECT count(*) FROM v3_billing.accounts`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("refused second target acquired money count=%d err=%v", count, err)
	}
	if _, err := m.SyncOnline(ctx, opts); err != nil {
		t.Fatal("second target refusal corrupted the original migration", err)
	}
}

func TestOnlineRecoveryCaptureTargetBindingConcurrentCAS(t *testing.T) {
	_, source, _, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	if _, err := SetupOnlineCapture(ctx, source, opts.RunID, true); err != nil {
		t.Fatal(err)
	}
	type result struct {
		identity string
		err      error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for _, identity := range []string{strings.Repeat("a", 64), strings.Repeat("b", 64)} {
		go func(identity string) {
			<-start
			results <- result{identity, BindOnlineCaptureTarget(ctx, source, opts.RunID, identity)}
		}(identity)
	}
	close(start)
	var winner string
	var failures int
	for i := 0; i < 2; i++ {
		r := <-results
		if r.err == nil {
			if winner != "" {
				t.Fatal("both distinct targets acquired the same capture stream")
			}
			winner = r.identity
		} else {
			failures++
		}
	}
	if winner == "" || failures != 1 {
		t.Fatalf("CAS did not select exactly one target winner=%q failures=%d", winner, failures)
	}
	if err := BindOnlineCaptureTarget(ctx, source, opts.RunID, winner); err != nil {
		t.Fatal("same-target retry was not idempotent", err)
	}
	var stored string
	if err := source.QueryRow(ctx, `SELECT target_identity FROM v3_migration_capture.config`).Scan(&stored); err != nil || stored != winner {
		t.Fatalf("concurrent losing bind altered winner stored=%q winner=%q err=%v", stored, winner, err)
	}
}
