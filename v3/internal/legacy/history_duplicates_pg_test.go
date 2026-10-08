//go:build pgintegration

package legacy

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestHistoryDuplicateUsageIDsStreamScopedAndStable(t *testing.T) {
	source, target, crypto := importTestDB(t)
	historyFixture(t, source)
	ctx := context.Background()
	if _, err := source.Exec(ctx, `
	 INSERT INTO migration_source.users SELECT 8,'bob',password,role,status,"group",quota,500,setting FROM migration_source.users WHERE id=7;
	 INSERT INTO billing.accounts VALUES('wallet-8','user',8,'claude_wallet','quota');
	 INSERT INTO billing.balance_snapshots VALUES('wallet-8',500,0);
	 INSERT INTO migration_source.logs (id,user_id,created_at,type,quota,request_id,model_name,other)
	 VALUES (201,8,1700000000,2,3,'kept-request','chat-model','{}'),
	 (202,7,1700000001,2,3,'kept-request','chat-model','{}'),
	 (203,7,1700000000,6,3,'kept-request','chat-model','{}'),
	 (204,7,1700000020,2,3,'','chat-model','{}'),(205,7,1700000020,2,3,'','chat-model','{}'),
	 (206,7,1700000021,2,3,'only-one-usage','chat-model','{}'),
	 (207,7,1700000021,6,3,'only-one-usage','chat-model','{}');
	 INSERT INTO migration_source.logs (id,user_id,created_at,type,quota,request_id,model_name,other)
	 SELECT 1000000+g,7,1700001000,2,1,'mass-duplicate','chat-model','{}' FROM generate_series(1,3000) g`); err != nil {
		t.Fatal(err)
	}
	reader := readonlySource(t, source)
	sourceTx, err := reader.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sourceTx.Rollback(ctx) }()
	sources, err := discoverSources(ctx, sourceTx)
	if err != nil {
		t.Fatal(err)
	}
	d, err := loadHistory(ctx, sourceTx, sources)
	if err != nil || len(d.issues) != 0 || d.counts["usage_request_ids_disambiguated"] != 3002 {
		t.Fatalf("history counts=%v issues=%v err=%v", d.counts, d.issues, err)
	}
	users, err := loadUsers(ctx, sourceTx, sources)
	if err != nil {
		t.Fatal(err)
	}
	importer := NewImporter(reader, target, crypto)
	for i := 0; i < 2; i++ {
		if err = pgx.BeginFunc(ctx, target, func(tx pgx.Tx) error {
			if err := importer.importUsers(ctx, tx, users); err != nil {
				return err
			}
			return importer.importHistory(ctx, tx, d)
		}); err != nil {
			t.Fatalf("stable duplicate retry %d: %v", i, err)
		}
	}
	for _, expected := range []struct {
		id      int64
		user    int64
		request string
	}{
		{51, 7, "kept-request:v2-log:51"}, {54, 7, "kept-request:v2-log:54"},
		{201, 8, "kept-request"}, {202, 7, "kept-request"},
		{204, 7, "v2-log:204"}, {205, 7, "v2-log:205"}, {206, 7, "only-one-usage"},
		{1000001, 7, "mass-duplicate:v2-log:1000001"}, {1003000, 7, "mass-duplicate:v2-log:1003000"},
	} {
		var user int64
		var request string
		if err = target.QueryRow(ctx, `SELECT user_id,request_id FROM v3_billing.usage_logs WHERE id=$1`, expected.id).Scan(&user, &request); err != nil || user != expected.user || request != expected.request {
			t.Fatalf("log=%d user/request=%d/%s want=%d/%s err=%v", expected.id, user, request, expected.user, expected.request, err)
		}
	}
	var qualified, distinct, originals int64
	if err = target.QueryRow(ctx, `SELECT count(*),count(DISTINCT request_id),(SELECT count(*) FROM v3_audit.events WHERE id>=1000000 AND request_id='mass-duplicate') FROM v3_billing.usage_logs WHERE id>=1000000 AND request_id='mass-duplicate:v2-log:'||id::text`).Scan(&qualified, &distinct, &originals); err != nil || qualified != 3000 || distinct != 3000 || originals != 3000 {
		t.Fatalf("duplicate projections=%d/%d original audit IDs=%d err=%v", qualified, distinct, originals, err)
	}
	check := func() Report {
		r := Report{}
		if err := pgx.BeginTxFunc(ctx, target, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error { return importer.checkHistory(ctx, tx, d, &r) }); err != nil {
			t.Fatal(err)
		}
		return r
	}
	if r := check(); len(r.Issues) != 0 || r.Counts["check:history"] != 6027 {
		t.Fatalf("streamed exact check=%+v", r)
	}
	// Large mismatch sets retain a representative diagnostic and an exact
	// count, rather than allocating one Issue object per historical record.
	if _, err = target.Exec(ctx, `UPDATE v3_billing.usage_logs SET terminal='failed' WHERE id>=1000000`); err != nil {
		t.Fatal(err)
	}
	if r := check(); len(r.Issues) != 1 || r.Counts["check:history:usage_logs:mismatched"] != 3000 {
		t.Fatalf("bounded mismatch diagnostics=%+v", r)
	}
	if err = pgx.BeginFunc(ctx, target, func(tx pgx.Tx) error { return importer.importHistory(ctx, tx, d) }); err == nil {
		t.Fatal("tampered duplicate projection accepted on retry")
	}
	t.Log("3002 duplicate rows qualified while streaming; user/time/empty/nonusage boundaries retained; 3000 mismatches represented by one issue with exact count")
}

func TestHistoryInvalidRowsKeepBoundedDiagnosticsAndRejectImport(t *testing.T) {
	source, target, crypto := importTestDB(t)
	historyFixture(t, source)
	ctx := context.Background()
	if _, err := source.Exec(ctx, `INSERT INTO migration_source.logs (id,user_id,created_at,type,quota,request_id)
	 SELECT 1000000+g,7,1700001000,-1,1,'invalid-'||g FROM generate_series(1,4097) g`); err != nil {
		t.Fatal(err)
	}
	tx, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	sources, err := discoverSources(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	d, err := loadHistory(ctx, tx, sources)
	if err != nil {
		t.Fatal(err)
	}
	r := Report{}
	d.validate(&r)
	if len(r.Issues) != 1 || r.Issues[0].Entity != "logs" || r.Counts["history.invalid_rows.logs.invalid_history"] != 4097 || r.Counts["history.logs"] != 4101 {
		t.Fatalf("invalid row diagnostics=%+v", r)
	}
	importer := NewImporter(source, target, crypto)
	if err = pgx.BeginFunc(ctx, target, func(out pgx.Tx) error { return importer.importHistory(ctx, out, d) }); err == nil {
		t.Fatal("invalid history import accepted")
	}
	var count int64
	if err = target.QueryRow(ctx, `SELECT count(*) FROM v3_audit.events`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid import wrote events count=%d err=%v", count, err)
	}
}

func TestHistoryUsageRequestIDFallback(t *testing.T) {
	for _, test := range []struct {
		request   string
		duplicate bool
		want      string
	}{{"original", false, "original"}, {"original", true, "original:v2-log:42"}, {"", false, "v2-log:42"}, {"", true, "v2-log:42"}} {
		if got := historyUsageRequestID(historyLog{ID: 42, RequestID: test.request}, test.duplicate); got != test.want {
			t.Fatal(fmt.Sprintf("request=%s duplicate=%v got=%s want=%s", test.request, test.duplicate, got, test.want))
		}
	}
	// Decoding preserves the original record; the duplicate flag is not
	// injected into source JSON or the typed audit projection.
	log, err := decodeHistoryLog(json.RawMessage(`{"id":42,"user_id":7,"type":2,"request_id":"original","quota":1}`))
	if err != nil || log.RequestID != "original" {
		t.Fatalf("original request changed: %+v err=%v", log, err)
	}
}
