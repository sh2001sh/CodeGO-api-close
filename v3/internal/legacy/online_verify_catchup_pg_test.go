//go:build pgintegration

package legacy

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func onlineCatchupReady(t *testing.T, m *Importer, opts OnlineOptions) {
	t.Helper()
	ctx := context.Background()
	if _, err := m.PrepareOnline(ctx, opts, true); err != nil {
		t.Fatal(err)
	}
	if r, err := m.CopyOnline(ctx, opts); err != nil || r.Phase != "copied" {
		t.Fatalf("copy=%+v err=%v", r, err)
	}
	onlineMigrationSync(t, m, opts)
}

func TestOnlineVerifyCatchupFundingAndExactAmounts(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, true)
	ctx := context.Background()
	onlineCatchupReady(t, m, opts)
	onlineRecoveryExec(t, source, `UPDATE migration_source.logs SET quota=9007199254740993,content='snapshot catchup' WHERE id=51;
 UPDATE billing.funding_lots SET remaining_amount=55 WHERE lot_id='funding-box-lot';
 UPDATE billing.funding_allocations SET amount=45 WHERE allocation_id='funding-box-allocation'`)
	if _, err := m.VerifyOnline(ctx, opts); err == nil {
		t.Fatal("ordinary verification accepted pending changes")
	}
	r, err := m.VerifyOnlineCatchup(ctx, opts)
	if err != nil || !r.Applied || r.Phase != "verified" || r.Acknowledged != 3 || r.Pending != 0 {
		t.Fatalf("catchup=%+v err=%v", r, err)
	}
	var amount string
	if err := target.QueryRow(ctx, "SELECT amount::text FROM "+onlineStage("v3_billing.usage_logs")+" WHERE id=51").Scan(&amount); err != nil || amount != "18014398509481986" {
		t.Fatalf("exact amount=%s err=%v", amount, err)
	}
	onlineRecoveryAssertNoMoney(t, target)
	if _, err := m.FinalizeOnline(ctx, opts); err == nil {
		t.Fatal("catchup verification bypassed the production fence")
	}
	if _, err := m.VerifyOnline(ctx, opts); err != nil {
		t.Fatal(err)
	}
}

func TestOnlineVerifyCatchupBudgetRollback(t *testing.T) {
	for _, size := range []string{"events", "bytes"} {
		t.Run(size, func(t *testing.T) {
			m, source, target, opts := onlineMigrationFixture(t, false)
			onlineCatchupReady(t, m, opts)
			before := onlineRecoveryTotals(t, target)
			progress := channelRebindProgress(t, target)
			if size == "events" {
				onlineRecoveryExec(t, source, `UPDATE migration_source.logs SET content='too many events' WHERE id=51;
 INSERT INTO v3_migration_capture.events(table_name,row_key)
 SELECT e.table_name,e.row_key FROM (SELECT table_name,row_key FROM v3_migration_capture.events WHERE NOT acked LIMIT 1)e CROSS JOIN generate_series(1,16384)g`)
			} else {
				onlineRecoveryExec(t, source, `UPDATE migration_source.logs SET content='too many bytes' WHERE id=51;
 INSERT INTO v3_migration_capture.events(table_name,row_key)
 SELECT table_name,jsonb_build_object('id',51,'padding',repeat('x',3*1024*1024)) FROM v3_migration_capture.events WHERE NOT acked LIMIT 1;
 INSERT INTO v3_migration_capture.events(table_name,row_key)
 SELECT table_name,jsonb_build_object('id',51,'padding',repeat('x',2*1024*1024)) FROM v3_migration_capture.events WHERE NOT acked LIMIT 1`)
			}
			r, err := m.VerifyOnlineCatchup(context.Background(), opts)
			if err == nil || !strings.Contains(err.Error(), "exceeds bounded event budget") || r.Applied || r.Acknowledged != 0 {
				t.Fatalf("budget accepted report=%+v err=%v", r, err)
			}
			if onlineRecoveryTotals(t, target) != before || channelRebindProgress(t, target) != progress {
				t.Fatal("rejected budget changed committed staging receipts")
			}
			var acked int64
			if err := source.QueryRow(context.Background(), "SELECT count(*) FROM v3_migration_capture.events WHERE acked").Scan(&acked); err != nil || acked != 0 {
				t.Fatalf("rejected budget ACK=%d err=%v", acked, err)
			}
			onlineRecoveryAssertNoMoney(t, target)
		})
	}
}

func TestOnlineVerifyCatchupLateFailureRollsBackProjectionAndAck(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	onlineCatchupReady(t, m, opts)
	before := onlineRecoveryTotals(t, target)
	onlineRecoveryExec(t, target, `CREATE FUNCTION v3_migration_online.reject_catchup_verify() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'injected late verification failure'; END$$;
 CREATE TRIGGER reject_catchup_verify BEFORE UPDATE ON v3_migration_online.run FOR EACH ROW WHEN(NEW.phase='verified') EXECUTE FUNCTION v3_migration_online.reject_catchup_verify()`)
	onlineRecoveryExec(t, source, `UPDATE migration_source.logs SET quota=57 WHERE id=51`)
	r, err := m.VerifyOnlineCatchup(context.Background(), opts)
	if err == nil || r.Applied || r.Acknowledged != 0 {
		t.Fatalf("late error committed report=%+v err=%v", r, err)
	}
	if onlineRecoveryTotals(t, target) != before {
		t.Fatal("late failure changed totals")
	}
	var pending int64
	if err := source.QueryRow(context.Background(), "SELECT count(*) FROM v3_migration_capture.events WHERE NOT acked").Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("late failure pending=%d err=%v", pending, err)
	}
	onlineRecoveryExec(t, target, `DROP TRIGGER reject_catchup_verify ON v3_migration_online.run`)
	if r, err := m.VerifyOnlineCatchup(context.Background(), opts); err != nil || !r.Applied || r.Acknowledged != 1 {
		t.Fatalf("retry=%+v err=%v", r, err)
	}
	onlineRecoveryAssertNoMoney(t, target)
}

func TestOnlineVerifyCatchupLateLowIDCommitRemainsPending(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	onlineCatchupReady(t, m, opts)
	late, err := source.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = late.Rollback(context.Background()) }()
	if _, err := late.Exec(ctx, `UPDATE migration_source.logs SET quota=59 WHERE id=51`); err != nil {
		t.Fatal(err)
	}
	onlineRecoveryExec(t, source, `UPDATE migration_source.logs SET content='visible higher ID' WHERE id=54`)
	barrier := &onlineFenceBarrier{arrived: make(chan struct{}), release: make(chan struct{}), queryPrefix: "UPDATE v3_migration_online.run SET phase='verified'"}
	defer barrier.unblock()
	config := target.Config().Copy()
	config.ConnConfig.Tracer = barrier
	traced, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer traced.Close()
	clone := *m
	clone.pool = traced
	type outcome struct {
		report OnlineReport
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		r, err := clone.VerifyOnlineCatchup(ctx, opts)
		done <- outcome{r, err}
	}()
	select {
	case <-barrier.arrived:
	case <-ctx.Done():
		t.Fatal("verification did not reach the late-commit barrier")
	}
	if err := late.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	barrier.unblock()
	result := <-done
	if result.err != nil || !result.report.Applied || result.report.Acknowledged != 1 || result.report.Pending != 1 {
		t.Fatalf("snapshot catchup=%+v err=%v", result.report, result.err)
	}
	if _, err := m.VerifyOnline(ctx, opts); err == nil {
		t.Fatal("later low-ID event disappeared")
	}
	onlineMigrationSync(t, m, opts)
	if _, err := m.VerifyOnline(ctx, opts); err != nil {
		t.Fatal(err)
	}
	var amount int64
	if err := target.QueryRow(ctx, "SELECT amount FROM "+onlineStage("v3_billing.usage_logs")+" WHERE id=51").Scan(&amount); err != nil || amount != 118 {
		t.Fatalf("later commit amount=%d err=%v", amount, err)
	}
	onlineRecoveryAssertNoMoney(t, target)
}

func TestOnlineVerifyCatchupRejectsWrongAdministrator(t *testing.T) {
	m, _, target, opts := onlineMigrationFixture(t, false)
	onlineCatchupReady(t, m, opts)
	opts.SourceAdmin = target
	if r, err := m.VerifyOnlineCatchup(context.Background(), opts); err == nil || r.Applied {
		t.Fatalf("wrong source administrator=%+v err=%v", r, err)
	}
	opts.SourceAdmin = nil
	if _, err := (&Importer{}).VerifyOnlineCatchup(context.Background(), opts); err == nil {
		t.Fatal("nil acknowledgement pool accepted")
	}
	onlineRecoveryAssertNoMoney(t, target)
}

func TestOnlineVerifyCatchupRejectsIncompleteBaseline(t *testing.T) {
	m, _, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	onlineCatchupReady(t, m, opts)
	if err := pgx.BeginFunc(ctx, target, func(tx pgx.Tx) error {
		if err := onlineAuthorize(ctx, tx, opts.RunID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "UPDATE v3_migration_online.progress SET complete=false WHERE name='logs'")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if r, err := m.VerifyOnlineCatchup(ctx, opts); err == nil || r.Applied || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("incomplete catchup=%+v err=%v", r, err)
	}
	onlineRecoveryAssertNoMoney(t, target)
}
