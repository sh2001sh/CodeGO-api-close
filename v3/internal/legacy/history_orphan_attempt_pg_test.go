//go:build pgintegration

package legacy

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestHistoryOrphanAttemptsArchivedExactlyWithoutInventedParents(t *testing.T) {
	source, target, crypto := importTestDB(t)
	historyFixture(t, source)
	ctx := context.Background()
	if _, err := source.Exec(ctx, `ALTER TABLE gateway.request_attempt_audits ADD COLUMN legacy_extra jsonb;
	 INSERT INTO gateway.request_attempt_audits
	 SELECT * FROM jsonb_populate_record(NULL::gateway.request_attempt_audits,
	  (SELECT to_jsonb(a)||jsonb_build_object('attempt_id','orphan-attempt','request_id','absent-parent',
	   'legacy_extra',jsonb_build_object('exact_int',9007199254740993,'nested',jsonb_build_array('kept',null,true)))
	   FROM gateway.request_attempt_audits a WHERE attempt_id='kept-attempt'));
	 UPDATE gateway.request_audits SET status='in_flight',completed_at='0001-01-01T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	read, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = read.Rollback(ctx) }()
	if _, err = read.Exec(ctx, `SET LOCAL TimeZone='Europe/Berlin'`); err != nil {
		t.Fatal(err)
	}
	sources, err := discoverSources(ctx, read)
	if err != nil {
		t.Fatal(err)
	}
	history, err := loadHistory(ctx, read, sources)
	if err != nil {
		t.Fatal(err)
	}
	report := Report{}
	history.validate(&report)
	if len(report.Issues) != 0 || history.counts["request_attempt_audits"] != 2 ||
		history.counts["request_attempt_audits_linked"] != 1 || history.counts["orphan_request_attempt_history"] != 1 {
		t.Fatalf("archive classification: %+v", report)
	}
	var empty int
	if err = target.QueryRow(ctx, `SELECT count(*) FROM v3_audit.orphan_request_attempt_history`).Scan(&empty); err != nil || empty != 0 {
		t.Fatalf("validation wrote archive: %d, %v", empty, err)
	}
	users, err := loadUsers(ctx, read, sources)
	if err != nil {
		t.Fatal(err)
	}
	importer := NewImporter(source, target, crypto)
	for i := 0; i < 2; i++ {
		if err = pgx.BeginFunc(ctx, target, func(out pgx.Tx) error {
			if err := importer.importUsers(ctx, out, users); err != nil {
				return err
			}
			return importer.importHistory(ctx, out, history)
		}); err != nil {
			t.Fatalf("archive import %d: %v", i, err)
		}
	}
	var exact bool
	if err = target.QueryRow(ctx, `SELECT request_id='absent-parent'
	 AND source_record->'legacy_extra'->>'exact_int'='9007199254740993'
	 AND source_record->'legacy_extra'->'nested'='["kept",null,true]'::jsonb
	 FROM v3_audit.orphan_request_attempt_history WHERE attempt_id='orphan-attempt'`).Scan(&exact); err != nil || !exact {
		t.Fatalf("original fields changed: %v, %v", exact, err)
	}
	var requests, attempts, archives, entries, balance int64
	if err = target.QueryRow(ctx, `SELECT (SELECT count(*) FROM v3_audit.request_audits),
	 (SELECT count(*) FROM v3_audit.request_attempt_audits),(SELECT count(*) FROM v3_audit.orphan_request_attempt_history),
	 (SELECT count(*) FROM v3_billing.ledger_entries),balance FROM v3_billing.accounts
	 WHERE owner_type='user' AND owner_id=7 AND kind='wallet'`).Scan(&requests, &attempts, &archives, &entries, &balance); err != nil ||
		requests != 1 || attempts != 1 || archives != 1 || entries != 1 || balance != 1000 {
		t.Fatalf("archive altered parents/money: requests=%d attempts=%d archives=%d entries=%d balance=%d err=%v", requests, attempts, archives, entries, balance, err)
	}
	check := func() Report {
		t.Helper()
		r := Report{}
		if err := pgx.BeginTxFunc(ctx, target, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(out pgx.Tx) error {
			return importer.checkHistory(ctx, out, history, &r)
		}); err != nil {
			t.Fatal(err)
		}
		return r
	}
	if report = check(); len(report.Issues) != 0 || report.Counts["check:history:orphan_request_attempt_history:actual"] != 1 {
		t.Fatalf("archive check: %+v", report)
	}
	if _, err = target.Exec(ctx, `UPDATE v3_audit.orphan_request_attempt_history
	 SET source_record=source_record||'{"legacy_extra":{"exact_int":9007199254740992}}'::jsonb
	 WHERE attempt_id='orphan-attempt'`); err != nil {
		t.Fatal(err)
	}
	if report = check(); report.Counts["check:history:orphan_request_attempt_history:mismatched"] != 1 {
		t.Fatalf("mutated original archive was accepted: %+v", report)
	}
	if err = pgx.BeginFunc(ctx, target, func(out pgx.Tx) error { return importer.importHistory(ctx, out, history) }); err == nil {
		t.Fatal("changed archive silently overwritten on replay")
	}
	// Archival classification does not exempt malformed original attempts from
	// field validation, even when their parent is already missing.
	if _, err = source.Exec(ctx, `UPDATE gateway.request_attempt_audits SET duration_ms=-1 WHERE attempt_id='orphan-attempt'`); err != nil {
		t.Fatal(err)
	}
	if err = read.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	read, err = source.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = read.Rollback(ctx) }()
	history, err = loadHistory(ctx, read, sources)
	if err != nil {
		t.Fatal(err)
	}
	report = Report{}
	history.validate(&report)
	if report.Counts["history.invalid_rows.request_attempt_audits.invalid_history"] != 1 || len(report.Issues) == 0 {
		t.Fatalf("invalid orphan was accepted: %+v", report)
	}
}
