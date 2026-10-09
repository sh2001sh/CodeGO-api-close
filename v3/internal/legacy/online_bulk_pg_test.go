//go:build pgintegration

package legacy

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

type onlineBulkCountingTx struct {
	pgx.Tx
	retiredDeletes, parentQueries int
}

func (t *onlineBulkCountingTx) count(query string) {
	if strings.Contains(query, "DELETE FROM v3_migration_online.retired_rows") {
		t.retiredDeletes++
	}
	if strings.Contains(query, onlineStage("v3_audit.request_audits")) &&
		(strings.HasPrefix(query, "SELECT request_id FROM ") || strings.HasPrefix(query, "SELECT EXISTS(SELECT 1 FROM ")) {
		t.parentQueries++
	}
}

func (t *onlineBulkCountingTx) Query(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
	t.count(query)
	return t.Tx.Query(ctx, query, args...)
}

func (t *onlineBulkCountingTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	t.count(query)
	return t.Tx.QueryRow(ctx, query, args...)
}

func TestOnlineBulkQueriesStayBoundedAndPreserveExactProjection(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	onlineRecoveryExec(t, source, `INSERT INTO billing.ledger_entries
 SELECT x.* FROM billing.ledger_entries a CROSS JOIN generate_series(1,510)g
 CROSS JOIN LATERAL jsonb_populate_record(NULL::billing.ledger_entries,
 to_jsonb(a)||jsonb_build_object('entry_id','bulk-retired-'||g,'idempotency_key','bulk-retired-key-'||g))x
 WHERE a.entry_id='retired-points-entry'`)
	onlineRecoveryExec(t, source, `INSERT INTO gateway.request_attempt_audits
	 SELECT x.* FROM gateway.request_attempt_audits a CROSS JOIN generate_series(1,511)g
 CROSS JOIN LATERAL jsonb_populate_record(NULL::gateway.request_attempt_audits,
 to_jsonb(a)||jsonb_build_object('attempt_id','bulk-attempt-'||g,
 'request_id',CASE WHEN g%2=0 THEN 'kept-request' ELSE 'bulk-missing-parent' END))x
 WHERE a.attempt_id='kept-attempt'`)
	onlineMigrationReady(t, m, opts)
	read, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = read.Rollback(ctx) }()
	write, err := target.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = write.Rollback(ctx) }()
	if err := onlineAuthorize(ctx, write, opts.RunID); err != nil {
		t.Fatal(err)
	}
	sources, specs, err := onlineBindings(ctx, read, write, opts.RunID)
	if err != nil {
		t.Fatal(err)
	}
	counted := &onlineBulkCountingTx{Tx: write}
	p, err := loadOnlineProjector(ctx, read, counted, sources)
	if err != nil {
		t.Fatal(err)
	}
	var before, after string
	totalsQuery := "SELECT jsonb_object_agg(name,value::text)::text FROM v3_migration_online.totals"
	if err := write.QueryRow(ctx, totalsQuery).Scan(&before); err != nil {
		t.Fatal(err)
	}
	for _, spec := range specs {
		if spec.name != "ledger_entries" && spec.name != "request_attempt_audits" && spec.name != "logs" {
			continue
		}
		var cursor []byte
		maxRows := 0
		for {
			inputs, err := onlineReadBatch(ctx, read, spec, cursor)
			if err != nil {
				t.Fatal(err)
			}
			if len(inputs) == 0 {
				break
			}
			if len(inputs) > maxRows {
				maxRows = len(inputs)
			}
			counted.retiredDeletes, counted.parentQueries = 0, 0
			if err := p.apply(ctx, spec, inputs); err != nil {
				t.Fatal(err)
			}
			parents := 0
			if spec.name == "request_attempt_audits" {
				parents = 1
			}
			if counted.retiredDeletes != 1 || counted.parentQueries != parents {
				t.Fatalf("%s rows=%d retired deletes=%d parent queries=%d", spec.name, len(inputs), counted.retiredDeletes, counted.parentQueries)
			}
			cursor = inputs[len(inputs)-1].key
		}
		if spec.name != "logs" && maxRows != 512 {
			t.Fatalf("%s did not exercise a full bounded batch: %d", spec.name, maxRows)
		}
		counted.retiredDeletes, counted.parentQueries = 0, 0
		if err := p.insert(ctx, spec, nil); err != nil || counted.retiredDeletes != 0 || counted.parentQueries != 0 {
			t.Fatalf("empty batch queried target: %v %+v", err, counted)
		}
	}
	if err := write.QueryRow(ctx, totalsQuery).Scan(&after); err != nil || before != after {
		t.Fatalf("batch replacement changed exact totals: %v", err)
	}
	var linked, orphan, changedRaw int
	query := "SELECT (SELECT count(*) FROM " + onlineStage("v3_audit.request_attempt_audits") + " WHERE attempt_id LIKE 'bulk-attempt-%')," +
		"(SELECT count(*) FROM " + onlineStage("v3_audit.orphan_request_attempt_history") + " WHERE attempt_id LIKE 'bulk-attempt-%')," +
		"(SELECT count(*) FROM " + onlineStage("v3_audit.orphan_request_attempt_history") + " WHERE attempt_id LIKE 'bulk-attempt-%' AND (source_record->>'request_id'<>'bulk-missing-parent' OR source_record->>'model_name'<>'chat-model'))"
	if err := write.QueryRow(ctx, query).Scan(&linked, &orphan, &changedRaw); err != nil || linked != 255 || orphan != 256 || changedRaw != 0 {
		t.Fatalf("linked/orphan projection linked=%d orphan=%d changedRaw=%d err=%v", linked, orphan, changedRaw, err)
	}
	t.Log("512-row batches use one retired DELETE and one attempt parent query; exact totals and linked/orphan projections preserved")
}

func TestOnlineBulkRetiredReplacementFailureRollsBackAndLeavesEventsPending(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	onlineMigrationReady(t, m, opts)
	before := onlineRecoveryTotals(t, target)
	onlineRecoveryExec(t, source, "UPDATE billing.ledger_entries SET amount=5 WHERE entry_id='retired-points-entry'")
	onlineRecoveryExec(t, source, "DELETE FROM billing.ledger_entries WHERE entry_id='retired-gpt-entry'")
	onlineRecoveryExec(t, source, "UPDATE migration_source.logs SET quota=-1 WHERE id=54")
	if _, err := m.SyncOnline(ctx, opts); err == nil {
		t.Fatal("invalid later projection was accepted")
	}
	var receipts, pending, acked int
	if err := target.QueryRow(ctx, "SELECT count(*) FROM v3_migration_online.retired_rows WHERE name='ledger_entries'").Scan(&receipts); err != nil || receipts != 2 || before != onlineRecoveryTotals(t, target) {
		t.Fatalf("failed projection changed retired receipts/totals: receipts=%d err=%v", receipts, err)
	}
	if err := source.QueryRow(ctx, "SELECT count(*) FILTER(WHERE NOT acked),count(*) FILTER(WHERE acked) FROM v3_migration_capture.events").Scan(&pending, &acked); err != nil || pending != 3 || acked != 0 {
		t.Fatalf("failed projection acknowledged source events: pending=%d acked=%d err=%v", pending, acked, err)
	}
	onlineRecoveryExec(t, source, "UPDATE migration_source.logs SET quota=10 WHERE id=54")
	onlineMigrationSync(t, m, opts)
	if _, err := m.VerifyOnline(ctx, opts); err != nil {
		t.Fatal(err)
	}
	var amount string
	if err := target.QueryRow(ctx, "SELECT value::text FROM v3_migration_online.totals WHERE name='retired.history.retired:ledger_entries.amount_v2_units'").Scan(&amount); err != nil || amount != "5" {
		t.Fatalf("retired replacement lost exact decrement: amount=%s err=%v", amount, err)
	}
	onlineRecoveryAssertNoMoney(t, target)
}
