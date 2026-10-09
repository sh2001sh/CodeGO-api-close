//go:build pgintegration

package legacy

import (
	"context"
	"strings"
	"testing"
)

func TestOnlineBatchLargeRowsCopyAndSync(t *testing.T) {
	for _, payload := range []string{"plain", "html_escape"} {
		t.Run(payload, func(t *testing.T) {
			m, source, target, opts := onlineMigrationFixture(t, false)
			ctx := context.Background()
			char, count := "b", 2300000
			if payload == "html_escape" {
				char, count = "<", 900000
			}
			onlineRecoveryExec(t, source, "UPDATE migration_source.logs SET content=repeat($1,$2) WHERE id IN(51,54)", char, count)
			onlineMigrationReady(t, m, opts)
			onlineRecoveryExec(t, source, "UPDATE migration_source.logs SET content='after'||repeat($1,$2) WHERE id IN(51,54)", char, count)
			report, err := m.SyncOnline(ctx, opts)
			if err != nil || report.Acknowledged != 2 || report.Pending != 0 || report.Tables["logs"] != 2 {
				t.Fatalf("large-row sync %+v %v", report, err)
			}
			var matches int
			if err := target.QueryRow(ctx, "SELECT count(*) FROM "+onlineStage("v3_audit.events")+" WHERE id IN(51,54) AND content='after'||repeat($1,$2)", char, count).Scan(&matches); err != nil || matches != 2 {
				t.Fatalf("large rows changed or omitted: matches=%d err=%v", matches, err)
			}
			if _, err := m.VerifyOnline(ctx, opts); err != nil {
				t.Fatal(err)
			}
			onlineRecoveryAssertNoMoney(t, target)
		})
	}
}

func TestOnlineBatchLaterInvalidRowRollsBackEarlierBatchesAndAcknowledgements(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	onlineMigrationReady(t, m, opts)
	beforeTotals := onlineRecoveryTotals(t, target)
	var before string
	if err := target.QueryRow(ctx, "SELECT md5(string_agg(id::text||content||amount::text,',' ORDER BY id)) FROM "+onlineStage("v3_audit.events")).Scan(&before); err != nil {
		t.Fatal(err)
	}
	// Key order is deterministic: valid 51 fills the first byte-bounded batch;
	// invalid 54 is decoded only after that first batch has been applied.
	onlineRecoveryExec(t, source, "UPDATE migration_source.logs SET content=repeat('x',2300000) WHERE id IN(51,54)")
	onlineRecoveryExec(t, source, "UPDATE migration_source.logs SET quota=-1 WHERE id=54")
	if _, err := m.SyncOnline(ctx, opts); err == nil {
		t.Fatal("later invalid usage was accepted")
	}
	var after string
	if err := target.QueryRow(ctx, "SELECT md5(string_agg(id::text||content||amount::text,',' ORDER BY id)) FROM "+onlineStage("v3_audit.events")).Scan(&after); err != nil || before != after {
		t.Fatalf("earlier batch escaped rollback before=%s after=%s err=%v", before, after, err)
	}
	if afterTotals := onlineRecoveryTotals(t, target); beforeTotals != afterTotals {
		t.Fatal("failed later batch changed receipt totals")
	}
	var pending, acknowledged int
	if err := source.QueryRow(ctx, "SELECT count(*) FILTER(WHERE NOT acked),count(*) FILTER(WHERE acked) FROM v3_migration_capture.events").Scan(&pending, &acknowledged); err != nil || pending != 3 || acknowledged != 0 {
		t.Fatalf("failed batch acknowledged source pending=%d ack=%d err=%v", pending, acknowledged, err)
	}
	onlineRecoveryExec(t, source, "UPDATE migration_source.logs SET quota=10 WHERE id=54")
	onlineMigrationSync(t, m, opts)
	if _, err := m.VerifyOnline(ctx, opts); err != nil {
		t.Fatal(err)
	}
	onlineRecoveryAssertNoMoney(t, target)
}

func TestOnlineBatchStreamsMoreThan512ParentRelatedAttemptsWithoutDoubleCounting(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	onlineRecoveryExec(t, source, `INSERT INTO gateway.request_attempt_audits
 SELECT x.* FROM gateway.request_attempt_audits a CROSS JOIN generate_series(1,600)g
 CROSS JOIN LATERAL jsonb_populate_record(NULL::gateway.request_attempt_audits,
 to_jsonb(a)||jsonb_build_object('attempt_id','batch-attempt-'||lpad(g::text,4,'0'),'request_id','batch-parent'))x
 WHERE a.attempt_id='kept-attempt'`)
	onlineMigrationReady(t, m, opts)
	onlineRecoveryExec(t, source, `INSERT INTO gateway.request_audits SELECT x.* FROM gateway.request_audits a
 CROSS JOIN LATERAL jsonb_populate_record(NULL::gateway.request_audits,to_jsonb(a)||'{"request_id":"batch-parent"}'::jsonb)x
 WHERE a.request_id='kept-request'`)
	// The direct dirty child is also related to the new parent. It must not be
	// replayed/counted twice while the other 599 children stream across batches.
	onlineRecoveryExec(t, source, "UPDATE gateway.request_attempt_audits SET attempt_no=9 WHERE attempt_id='batch-attempt-0001'")
	report, err := m.SyncOnline(ctx, opts)
	if err != nil || report.Acknowledged != 2 || report.Pending != 0 || report.Tables["request_attempt_audits"] != 600 {
		t.Fatalf("many-child insertion %+v %v", report, err)
	}
	var linked, orphan int
	query := "SELECT (SELECT count(*) FROM " + onlineStage("v3_audit.request_attempt_audits") + " WHERE request_id='batch-parent'),(SELECT count(*) FROM " + onlineStage("v3_audit.orphan_request_attempt_history") + " WHERE request_id='batch-parent')"
	if err := target.QueryRow(ctx, query).Scan(&linked, &orphan); err != nil || linked != 600 || orphan != 0 {
		t.Fatalf("parent insertion lost children linked=%d orphan=%d err=%v", linked, orphan, err)
	}
	onlineRecoveryExec(t, source, "DELETE FROM gateway.request_audits WHERE request_id='batch-parent'")
	report, err = m.SyncOnline(ctx, opts)
	if err != nil || report.Acknowledged != 1 || report.Pending != 0 || report.Tables["request_attempt_audits"] != 600 {
		t.Fatalf("many-child deletion %+v %v", report, err)
	}
	if err := target.QueryRow(ctx, query).Scan(&linked, &orphan); err != nil || linked != 0 || orphan != 600 {
		t.Fatalf("parent deletion lost children linked=%d orphan=%d err=%v", linked, orphan, err)
	}
	if _, err := m.VerifyOnline(ctx, opts); err != nil {
		t.Fatal(err)
	}
	onlineRecoveryAssertNoMoney(t, target)
}

func TestOnlineBatchEncodedSingleRowLimitIsExplicitAndLeavesCapturePending(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	onlineMigrationReady(t, m, opts)
	onlineRecoveryExec(t, source, "UPDATE migration_source.logs SET content=repeat('<',12*1024*1024) WHERE id=51")
	if _, err := m.SyncOnline(ctx, opts); err == nil || !strings.Contains(err.Error(), "encoded projection exceeds 64 MiB") {
		t.Fatalf("oversized encoded single row was not rejected clearly: %v", err)
	}
	var unchanged bool
	if err := target.QueryRow(ctx, "SELECT content='kept usage' FROM "+onlineStage("v3_audit.events")+" WHERE id=51").Scan(&unchanged); err != nil || !unchanged {
		t.Fatalf("oversized projection changed target unchanged=%t err=%v", unchanged, err)
	}
	var pending int
	if err := source.QueryRow(ctx, "SELECT count(*) FROM v3_migration_capture.events WHERE NOT acked").Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("oversized projection lost its capture pending=%d err=%v", pending, err)
	}
	onlineRecoveryAssertNoMoney(t, target)
}

func TestOnlineBatchUniqueValueExchangeAcrossLargeRowsAndLaterFailureRollback(t *testing.T) {
	for _, laterFailure := range []bool{false, true} {
		name := "valid_exchange"
		if laterFailure {
			name = "later_invalid_projection"
		}
		t.Run(name, func(t *testing.T) {
			m, source, target, opts := onlineMigrationFixture(t, false)
			ctx := context.Background()
			onlineMigrationReady(t, m, opts)
			beforeTotals := onlineRecoveryTotals(t, target)
			var before string
			query := "SELECT md5(string_agg(entry_id||idempotency_key||reason_detail,',' ORDER BY entry_id)) FROM " + onlineStage("v3_billing.historical_entries")
			if err := target.QueryRow(ctx, query).Scan(&before); err != nil {
				t.Fatal(err)
			}
			onlineRecoveryExec(t, source, `UPDATE billing.ledger_entries
 SET reason_detail=repeat('e',2300000),idempotency_key=CASE entry_id WHEN 'credit-original' THEN 'debit-key' ELSE 'credit-key' END
 WHERE entry_id IN('credit-original','debit-original')`)
			if laterFailure {
				// credit-original is inserted first; the later debit row is invalid
				// after the whole dirty-key set has already been deleted.
				onlineRecoveryExec(t, source, "UPDATE billing.ledger_entries SET direction='invalid' WHERE entry_id='debit-original'")
				if _, err := m.SyncOnline(ctx, opts); err == nil {
					t.Fatal("later invalid ledger projection was accepted")
				}
				var after string
				if err := target.QueryRow(ctx, query).Scan(&after); err != nil || before != after || beforeTotals != onlineRecoveryTotals(t, target) {
					t.Fatalf("two-phase replay failed to roll back rows or totals: %v", err)
				}
				var pending, acked int
				if err := source.QueryRow(ctx, "SELECT count(*) FILTER(WHERE NOT acked),count(*) FILTER(WHERE acked) FROM v3_migration_capture.events").Scan(&pending, &acked); err != nil || pending != 3 || acked != 0 {
					t.Fatalf("failed exchange lost pending events pending=%d acked=%d err=%v", pending, acked, err)
				}
				onlineRecoveryExec(t, source, "UPDATE billing.ledger_entries SET direction='debit' WHERE entry_id='debit-original'")
			}
			report, err := m.SyncOnline(ctx, opts)
			if err != nil || report.Pending != 0 || report.Tables["ledger_entries"] != 2 {
				t.Fatalf("legitimate unique exchange could not catch up: %+v %v", report, err)
			}
			if _, err := m.VerifyOnline(ctx, opts); err != nil {
				t.Fatal(err)
			}
			onlineRecoveryAssertNoMoney(t, target)
			if _, err := SealOnlineCapture(ctx, source, opts.RunID); err != nil {
				t.Fatal(err)
			}
			if report, err := m.FinalizeOnline(ctx, opts); err != nil || !report.Applied {
				t.Fatalf("unique-exchange finalization %+v %v", report, err)
			}
			if report, err := m.Check(ctx); err != nil || len(report.Issues) != 0 {
				t.Fatalf("independent final check failed %+v %v", report, err)
			}
		})
	}
}
