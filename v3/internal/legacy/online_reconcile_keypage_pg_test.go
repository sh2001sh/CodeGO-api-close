//go:build pgintegration

package legacy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// All writes and failure injection belong to disposable synthetic databases.
func retainedKeyPageSeed(t *testing.T, source *pgxpool.Pool) {
	t.Helper()
	onlineRecoveryExec(t, source, `INSERT INTO migration_source.logs
	 SELECT 10000+g,7,1700010000,2,'key pruning','alice','key','chat-model',4503599627370497,1,1,0,false,13,11,'default','','shared-prune','','{}'
	 FROM generate_series(1,8193)g`)
}

func TestOnlineRetainedTargetKeys4096ResumeWidthAndRetired(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	retainedKeyPageSeed(t, source)
	retainedCopiedFixture(t, m, opts)
	ro, err := target.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ro.Rollback(ctx) }()
	spec := onlineSpec{name: "logs", keys: []string{"id"}}
	var cursor json.RawMessage
	seen, pages := map[int64]bool{}, 0
	for {
		keys, err := reconcileTargetKeys(ctx, ro, spec, "v3_audit.events", cursor)
		if err != nil || len(keys) > onlineCopyBatchRows {
			t.Fatalf("target page rows=%d err=%v", len(keys), err)
		}
		if len(keys) == 0 {
			break
		}
		if pages < 2 && len(keys) != onlineCopyBatchRows {
			t.Fatalf("narrow-key page %d rows=%d want=4096", pages, len(keys))
		}
		for _, key := range keys {
			var fields map[string]int64
			if err := json.Unmarshal(key, &fields); err != nil {
				t.Fatal(err)
			}
			if seen[fields["id"]] {
				t.Fatalf("duplicate key on resume: %s", key)
			}
			seen[fields["id"]] = true
		}
		cursor = keys[len(keys)-1]
		pages++
	}
	if len(seen) != 8197 || pages != 3 {
		t.Fatalf("target traversal keys=%d pages=%d want=8197/3", len(seen), pages)
	}
	if err := ro.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	owned, err := target.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := onlineAuthorize(ctx, owned, opts.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := owned.Exec(ctx, `INSERT INTO v3_migration_online.retired_rows(name,row_key,metrics)
	 SELECT 'width-fixture',jsonb_build_object('group_id','group-'||g,'model','model'),'{}' FROM generate_series(1,8193)g`); err != nil {
		t.Fatal(err)
	}
	if err := owned.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	// Three unindexed composite keys exercise byte limits without PostgreSQL's
	// btree index tuple-size limit preventing the synthetic wide-key setup.
	onlineRecoveryExec(t, target, `CREATE TABLE v3_migration_online.v3_audit__key_width(id bigint,payload text);
	 INSERT INTO v3_migration_online.v3_audit__key_width SELECT g,repeat('w',(2<<20)+128) FROM generate_series(1,3)g;
	 CREATE TABLE v3_migration_online.v3_audit__key_oversized(id bigint,payload text);
	 INSERT INTO v3_migration_online.v3_audit__key_oversized VALUES(1,repeat('w',(64<<20)+128))`)
	ro, err = target.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ro.Rollback(ctx) }()
	retired := onlineSpec{name: "width-fixture", keys: []string{"group_id", "model"}}
	cursor, total := json.RawMessage(nil), 0
	for {
		keys, err := reconcileTargetKeys(ctx, ro, retired, "retired_rows", cursor)
		if err != nil || len(keys) > onlineCopyBatchRows {
			t.Fatalf("retired page rows=%d err=%v", len(keys), err)
		}
		if len(keys) == 0 {
			break
		}
		total += len(keys)
		cursor = keys[len(keys)-1]
	}
	if total != 8193 {
		t.Fatalf("retired traversal keys=%d want=8193", total)
	}
	wide := onlineSpec{name: "key-width", keys: []string{"id", "payload"}}
	cursor = nil
	for id := 1; id <= 3; id++ {
		keys, err := reconcileTargetKeys(ctx, ro, wide, "v3_audit.key_width", cursor)
		if err != nil || len(keys) != 1 || len(keys[0]) > exactBulkBytes {
			t.Fatalf("4MiB boundary page %d rows=%d err=%v", id, len(keys), err)
		}
		var fields struct{ ID int }
		if err := json.Unmarshal(keys[0], &fields); err != nil || fields.ID != id {
			t.Fatalf("byte boundary lost resume key: id=%d err=%v", fields.ID, err)
		}
		cursor = keys[0]
	}
	if keys, err := reconcileTargetKeys(ctx, ro, wide, "v3_audit.key_oversized", nil); err == nil || keys != nil {
		t.Fatalf("over-64MiB composite key accepted: err=%v", err)
	}
	onlineRecoveryAssertNoMoney(t, target)
	t.Log("8197 narrow keys in three 4096-bounded pages; retired JSON cursor, composite 4MiB resume and 64MiB rejection passed")
}

type retainedPruneEnvelopeTracer struct {
	maxKeys, calls atomic.Int64
	invalid        atomic.Bool
}

func (tracer *retainedPruneEnvelopeTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if !strings.HasPrefix(data.SQL, "DELETE FROM ") || !strings.Contains(data.SQL, "jsonb_populate_recordset(") {
		return ctx
	}
	tracer.calls.Add(1)
	if len(data.Args) != 1 {
		tracer.invalid.Store(true)
		return ctx
	}
	encoded, ok := data.Args[0].([]byte)
	var keys []json.RawMessage
	if !ok || len(encoded) > exactBulkBytes || json.Unmarshal(encoded, &keys) != nil || len(keys) > exactBulkRows {
		tracer.invalid.Store(true)
		return ctx
	}
	for {
		old := tracer.maxKeys.Load()
		if int64(len(keys)) <= old || tracer.maxKeys.CompareAndSwap(old, int64(len(keys))) {
			break
		}
	}
	return ctx
}
func (*retainedPruneEnvelopeTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func TestOnlineRetainedTargetKeys4096PruneChunksExactMoneyAndGroups(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, true)
	ctx := context.Background()
	retainedKeyPageSeed(t, source)
	retainedCopiedFixture(t, m, opts)
	beforeUntouched := retainedRowXmin(t, target, 18193)
	capture := retainedPauseFixture(t, source, opts)
	// The first target page has exactly 4096 missing keys, including the four
	// original fixture logs. The shared request group spans all eight chunks.
	onlineRecoveryExec(t, source, `DELETE FROM migration_source.logs WHERE id<=14092;
	 DELETE FROM billing.funding_lots WHERE lot_id='funding-retired-lot';
	 DELETE FROM billing.funding_allocations WHERE allocation_id='funding-retired-allocation'`)
	next := retainedNewEpochFixture(t, source, target, opts, capture)
	if _, err := m.PrepareRetainedOnline(ctx, next); err != nil {
		t.Fatal(err)
	}
	tracer := &retainedPruneEnvelopeTracer{}
	config := target.Config().Copy()
	config.ConnConfig.Tracer = tracer
	measured, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer measured.Close()
	copyImporter := *m
	copyImporter.pool = measured
	if r, err := copyImporter.ReconcileRetainedOnline(ctx, next.OnlineOptions); err != nil || r.Pruned != 4098 {
		t.Fatalf("chunked prune report=%+v err=%v", r, err)
	}
	if tracer.invalid.Load() || tracer.maxKeys.Load() != exactBulkRows || tracer.calls.Load() < 16 {
		t.Fatalf("delete envelope max=%d calls=%d invalid=%v", tracer.maxKeys.Load(), tracer.calls.Load(), tracer.invalid.Load())
	}
	var count int64
	var amount string
	if err := target.QueryRow(ctx, "SELECT count(*),sum(amount)::text FROM "+onlineStage("v3_billing.usage_logs")).Scan(&count, &amount); err != nil || count != 4101 || amount != "36938524143692816394" {
		t.Fatalf("exact usage count=%d amount=%s err=%v", count, amount, err)
	}
	if err := target.QueryRow(ctx, "SELECT count(*) FROM "+onlineStage("v3_billing.usage_logs")+" WHERE request_id NOT LIKE 'shared-prune:v2-log:%'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("cross-chunk duplicate grouping differs: count=%d err=%v", count, err)
	}
	if retainedRowXmin(t, target, 18193) != beforeUntouched {
		t.Fatal("pruning rewrote unaffected audit event")
	}
	onlineMigrationSync(t, &copyImporter, next.OnlineOptions)
	if _, err := copyImporter.VerifyOnline(ctx, next.OnlineOptions); err != nil {
		t.Fatal(err)
	}
	onlineRecoveryAssertNoMoney(t, target)
	t.Log("4096 missing log keys replayed in eight <=512-key chunks; paired usage, duplicate groups, >float64 exact money and retired funding totals passed full Verify without native money")
}

// Inject an actual PostgreSQL error after the preceding chunks have executed,
// without changing the bound target schema or weakening its identity checks.
type retainedPruneLateFailureTx struct {
	pgx.Tx
	fail *atomic.Bool
}

func (tx retainedPruneLateFailureTx) Query(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
	if tx.fail.Load() && strings.HasPrefix(query, "DELETE FROM "+onlineStage("v3_audit.events")) && len(args) == 1 {
		var keys []map[string]int64
		if data, ok := args[0].([]byte); ok {
			if err := json.Unmarshal(data, &keys); err != nil {
				return nil, err
			}
			for _, key := range keys {
				if key["id"] == 14092 {
					return tx.Tx.Query(ctx, "SELECT 1/0")
				}
			}
		}
	}
	return tx.Tx.Query(ctx, query, args...)
}

func TestOnlineRetainedTargetKeys4096LateDeleteRollbackResumeAndCapture(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	retainedKeyPageSeed(t, source)
	retainedCopiedFixture(t, m, opts)
	capture := retainedPauseFixture(t, source, opts)
	onlineRecoveryExec(t, source, "DELETE FROM migration_source.logs WHERE id<=14092")
	next := retainedNewEpochFixture(t, source, target, opts, capture)
	if _, err := m.PrepareRetainedOnline(ctx, next); err != nil {
		t.Fatal(err)
	}
	// Isolate the target-key half on already copied fixture rows; source changes
	// in the gap are exclusively the deletions this test is exercising.
	owned, err := target.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := onlineAuthorize(ctx, owned, next.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := owned.Exec(ctx, `UPDATE v3_migration_online.progress SET cursor='{"version":1,"source_done":true}' WHERE name='logs'`); err != nil {
		t.Fatal(err)
	}
	if err := owned.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	failLate := &atomic.Bool{}
	failLate.Store(true)
	beforeXmin, beforeTotals := retainedRowXmin(t, target, 51), onlineRecoveryTotals(t, target)
	var beforeCursor []byte
	if err := target.QueryRow(ctx, "SELECT cursor FROM v3_migration_online.progress WHERE name='logs'").Scan(&beforeCursor); err != nil {
		t.Fatal(err)
	}
	runPage := func() (OnlineReconcileReport, error) {
		read, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			return OnlineReconcileReport{}, err
		}
		defer func() { _ = read.Rollback(ctx) }()
		write, err := target.Begin(ctx)
		if err != nil {
			return OnlineReconcileReport{}, err
		}
		defer func() { _ = write.Rollback(ctx) }()
		if err := onlineAuthorize(ctx, write, next.RunID); err != nil {
			return OnlineReconcileReport{}, err
		}
		sources, specs, err := onlineBindings(ctx, read, write, next.RunID)
		if err != nil {
			return OnlineReconcileReport{}, err
		}
		p, err := loadOnlineProjector(ctx, read, write, sources)
		if err != nil {
			return OnlineReconcileReport{}, err
		}
		for _, spec := range specs {
			if spec.name != "logs" {
				continue
			}
			r, _, err := p.reconcilePage(ctx, read, retainedPruneLateFailureTx{Tx: write, fail: failLate}, next.RunID, spec)
			if err == nil {
				err = write.Commit(ctx)
			}
			return r, err
		}
		return OnlineReconcileReport{}, fmt.Errorf("fixture log spec missing")
	}
	if _, err := runPage(); err == nil || !strings.Contains(err.Error(), "division by zero") {
		t.Fatalf("late eighth-chunk failure not surfaced: %v", err)
	}
	var afterCursor []byte
	if err := target.QueryRow(ctx, "SELECT cursor FROM v3_migration_online.progress WHERE name='logs'").Scan(&afterCursor); err != nil || string(afterCursor) != string(beforeCursor) || retainedRowXmin(t, target, 51) != beforeXmin || onlineRecoveryTotals(t, target) != beforeTotals {
		t.Fatalf("late failed page preserved row/metrics/cursor changes: %v", err)
	}
	onlineRecoveryExec(t, source, "UPDATE migration_source.logs SET quota=19 WHERE id=18193")
	var pendingBefore, pendingAfter int64
	if err := source.QueryRow(ctx, "SELECT count(*) FROM v3_migration_capture.events WHERE NOT acked").Scan(&pendingBefore); err != nil || pendingBefore == 0 {
		t.Fatalf("new epoch capture not active: pending=%d err=%v", pendingBefore, err)
	}
	failLate.Store(false)
	if r, err := runPage(); err != nil || r.Pruned != 4096 {
		t.Fatalf("same-cursor resume report=%+v err=%v", r, err)
	}
	if err := source.QueryRow(ctx, "SELECT count(*) FROM v3_migration_capture.events WHERE NOT acked").Scan(&pendingAfter); err != nil || pendingAfter != pendingBefore {
		t.Fatalf("pruning acknowledged new capture: before=%d after=%d err=%v", pendingBefore, pendingAfter, err)
	}
	var saved []byte
	if err := target.QueryRow(ctx, "SELECT cursor FROM v3_migration_online.progress WHERE name='logs'").Scan(&saved); err != nil {
		t.Fatal(err)
	}
	var cursor onlineReconcileCursor
	if err := json.Unmarshal(saved, &cursor); err != nil || cursor.Pruned != 4096 {
		t.Fatalf("resume cursor=%s err=%v", saved, err)
	}
	var key map[string]int64
	if err := json.Unmarshal(cursor.TargetCursor, &key); err != nil || len(key) != 1 || key["id"] != 14092 {
		t.Fatalf("resume key=%s err=%v", cursor.TargetCursor, err)
	}
	if _, err := m.ReconcileRetainedOnline(ctx, next.OnlineOptions); err != nil {
		t.Fatal(err)
	}
	onlineMigrationSync(t, m, next.OnlineOptions)
	if _, err := m.VerifyOnline(ctx, next.OnlineOptions); err != nil {
		t.Fatal(err)
	}
	onlineRecoveryAssertNoMoney(t, target)
	t.Log("eighth delete chunk rolled back earlier chunks, usage, exact totals and cursor; same-cursor retry committed 4096 keys once; new capture remained unacked until Sync and full Verify")
}
