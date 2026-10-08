//go:build pgintegration

package legacy

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type historyQueryCounter struct{ calls atomic.Int64 }

func (c *historyQueryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.calls.Add(1)
	return ctx
}

func (*historyQueryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestHistoryExactBulkPreservesTypesReadOnlyAndAtomicConflict(t *testing.T) {
	_, target, _ := importTestDB(t)
	ctx := context.Background()
	if _, err := target.Exec(ctx, `CREATE TABLE v3_audit.history_bulk_test (
	 id bigint GENERATED ALWAYS AS IDENTITY, tag text NOT NULL, amount bigint NOT NULL,
	 metadata jsonb NOT NULL, ciphertext bytea, created_at timestamptz NOT NULL, note text, PRIMARY KEY(id,tag))`); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2024, 1, 2, 3, 4, 5, 123456000, time.FixedZone("offset", 8*3600))
	rows := []map[string]any{
		{"id": int64(9007199254740993), "tag": "first", "amount": int64(9223372036854775806), "metadata": json.RawMessage(`{"precise":9007199254740993,"nested":{"ok":true}}`), "ciphertext": []byte{0, 1, 127, 255}, "created_at": stamp, "note": nil},
		{"id": int64(9007199254740993), "tag": "second", "amount": int64(-9007199254740993), "metadata": map[string]any{"precise": int64(9007199254740993)}, "ciphertext": []byte{}, "created_at": stamp, "note": ""},
	}
	apply := func(batch []map[string]any) error {
		return pgx.BeginFunc(ctx, target, func(tx pgx.Tx) error {
			return insertExactBulk(ctx, tx, "v3_audit", "history_bulk_test", []string{"id", "tag"}, batch)
		})
	}
	for i := 0; i < 2; i++ {
		if err := apply(rows); err != nil {
			t.Fatal(err)
		}
	}
	var amount int64
	var binary []byte
	if err := target.QueryRow(ctx, `SELECT amount,ciphertext FROM v3_audit.history_bulk_test WHERE tag='first'`).Scan(&amount, &binary); err != nil || amount != 9223372036854775806 || !reflect.DeepEqual(binary, []byte{0, 1, 127, 255}) {
		t.Fatalf("precise amount/binary=%d/%v err=%v", amount, binary, err)
	}
	if err := pgx.BeginTxFunc(ctx, target, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		matches, err := checkExactBulk(ctx, tx, "v3_audit", "history_bulk_test", []string{"id", "tag"}, rows)
		if err != nil || !reflect.DeepEqual(matches, []bool{true, true}) {
			t.Fatalf("read-only typed check=%v err=%v", matches, err)
		}
		missing := map[string]any{}
		for key, value := range rows[0] {
			missing[key] = value
		}
		missing["tag"] = "missing"
		changed := map[string]any{}
		for key, value := range rows[0] {
			changed[key] = value
		}
		changed["amount"] = int64(9223372036854775804)
		matches, err = checkExactBulk(ctx, tx, "v3_audit", "history_bulk_test", []string{"id", "tag"}, []map[string]any{missing, rows[1], changed})
		if err != nil || !reflect.DeepEqual(matches, []bool{false, true, false}) {
			t.Fatalf("missing/changed/order check=%v err=%v", matches, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// A mismatch in a later batch rolls back an earlier successful batch.
	err := pgx.BeginFunc(ctx, target, func(tx pgx.Tx) error {
		first := map[string]any{}
		for key, value := range rows[0] {
			first[key] = value
		}
		first["tag"] = "must-roll-back"
		if err := insertExactBulk(ctx, tx, "v3_audit", "history_bulk_test", []string{"id", "tag"}, []map[string]any{first}); err != nil {
			return err
		}
		changed := map[string]any{}
		for key, value := range rows[0] {
			changed[key] = value
		}
		changed["note"] = "tampered"
		return insertExactBulk(ctx, tx, "v3_audit", "history_bulk_test", []string{"id", "tag"}, []map[string]any{changed})
	})
	if err == nil || !strings.Contains(err.Error(), "conflicts with target") {
		t.Fatalf("semantic conflict accepted: %v", err)
	}
	var count int64
	if err = target.QueryRow(ctx, `SELECT count(*) FROM v3_audit.history_bulk_test`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("earlier batch not rolled back count=%d err=%v", count, err)
	}
	if err := apply([]map[string]any{rows[0], {"id": int64(4)}}); err == nil {
		t.Fatal("inconsistent columns accepted")
	}
}

func TestHistoryBulkStreamsMultipleBatchesWithBoundedRoundTrips(t *testing.T) {
	source, target, crypto := importTestDB(t)
	historyFixture(t, source)
	ctx := context.Background()
	// Cross two full batches and a tail for every large history table.
	const extra = 1050
	if _, err := source.Exec(ctx, `
	 INSERT INTO migration_source.logs SELECT 10000+g,7,1700000010+g,2,'bulk','alice','kept-key','chat-model',g,11,7,3,true,13,11,'default','','bulk-'||g,'','{"cache_read_tokens":4}' FROM generate_series(1,1050) g;
	 INSERT INTO billing.ledger_entries SELECT 'bulk-entry-'||g,'wallet-7','request','bulk-'||g,'settle_debit','debit',g,NULL,'bulk-entry-key-'||g,'usage','','system','','{}','2024-01-01T00:00:00Z'::timestamptz FROM generate_series(1,1050) g;
	 INSERT INTO gateway.request_audits SELECT 'bulk-'||g,'',7,11,'chat-model','default','chat','text','succeeded',true,true,g,11,7,13,1,0,200,'','2024-01-01T00:00:00Z'::timestamptz,'2024-01-01T00:00:00Z'::timestamptz,'2024-01-01T00:00:00Z'::timestamptz,'2024-01-01T00:00:00Z'::timestamptz FROM generate_series(1,1050) g;
	 INSERT INTO gateway.request_attempt_audits SELECT 'bulk-attempt-'||g,'bulk-'||g,0,0,13,'chat-model','','text','succeeded',true,200,'','completed','2024-01-01T00:00:00Z'::timestamptz,'2024-01-01T00:00:00Z'::timestamptz,0,'2024-01-01T00:00:00Z'::timestamptz FROM generate_series(1,1050) g`); err != nil {
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
	if err != nil || len(d.issues) != 0 {
		t.Fatalf("load=%v err=%v", d, err)
	}
	users, err := loadUsers(ctx, sourceTx, sources)
	if err != nil {
		t.Fatal(err)
	}
	importer := NewImporter(reader, target, crypto)
	if err = pgx.BeginFunc(ctx, target, func(tx pgx.Tx) error { return importer.importUsers(ctx, tx, users) }); err != nil {
		t.Fatal(err)
	}
	counter := &historyQueryCounter{}
	config := target.Config().Copy()
	config.ConnConfig.Tracer = counter
	measured, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer measured.Close()
	started := time.Now()
	for i := 0; i < 2; i++ {
		if err = pgx.BeginFunc(ctx, measured, func(tx pgx.Tx) error { return importer.importHistory(ctx, tx, d) }); err != nil {
			t.Fatal(err)
		}
	}
	checked := Report{}
	if err = pgx.BeginTxFunc(ctx, measured, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error { return importer.checkHistory(ctx, tx, d, &checked) }); err != nil || len(checked.Issues) != 0 || checked.Counts["check:history"] != 14+5*extra {
		t.Fatalf("check=%+v err=%v", checked, err)
	}
	// Per-record implementations would make >20,000 queries here. This bound
	// permits metadata/identity/totals work but must stay proportional to batches.
	if calls := counter.calls.Load(); calls > 150 {
		t.Fatalf("history made %d target round trips for %d rows", calls, checked.Counts["check:history"])
	}
	var count, total, balance int64
	if err = target.QueryRow(ctx, `SELECT count(*),sum(amount),(SELECT balance FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=7 AND kind='wallet') FROM v3_billing.usage_logs`).Scan(&count, &total, &balance); err != nil || count != 2+extra || total != 120+extra*(extra+1) || balance != 1000 {
		t.Fatalf("exact doubled history/count/wallet=%d/%d/%d err=%v", count, total, balance, err)
	}
	t.Logf("history rows checked=%d target queries=%d two imports+readonly check=%s", checked.Counts["check:history"], counter.calls.Load(), time.Since(started))
	// Detect equal-total tampering, not just counts or aggregate amount changes.
	if _, err = target.Exec(ctx, `UPDATE v3_billing.usage_logs SET terminal='failed' WHERE id=10001`); err != nil {
		t.Fatal(err)
	}
	checked = Report{}
	if err = pgx.BeginTxFunc(ctx, target, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error { return importer.checkHistory(ctx, tx, d, &checked) }); err != nil || len(checked.Issues) != 1 {
		t.Fatalf("typed tampering not detected: %+v err=%v", checked, err)
	}
	if err = pgx.BeginFunc(ctx, target, func(tx pgx.Tx) error { return importer.importHistory(ctx, tx, d) }); err == nil || errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("tampered usage accepted: %v", err)
	}
}

func TestHistoryExactBulkUsesIndexOnLargeTarget(t *testing.T) {
	_, target, _ := importTestDB(t)
	ctx := context.Background()
	if _, err := target.Exec(ctx, `CREATE TABLE v3_audit.history_bulk_plan (
	 id bigint PRIMARY KEY, amount bigint NOT NULL, created_at timestamptz NOT NULL, metadata jsonb NOT NULL);
	 INSERT INTO v3_audit.history_bulk_plan SELECT g,g*2,'2024-01-01T00:00:00Z'::timestamptz,jsonb_build_object('g',g) FROM generate_series(1,100000) g;
	 ANALYZE v3_audit.history_bulk_plan`); err != nil {
		t.Fatal(err)
	}
	rows := make([]map[string]any, exactBulkRows)
	for i := range rows {
		id := int64((i + 1) * 193)
		rows[i] = map[string]any{"id": id, "amount": id * 2, "created_at": time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), "metadata": map[string]any{"g": id}}
	}
	name, columns, data, err := exactBulkInput("v3_audit", "history_bulk_plan", []string{"id"}, rows)
	if err != nil {
		t.Fatal(err)
	}
	var encoded json.RawMessage
	if err = target.QueryRow(ctx, "EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) "+exactBulkCheckQuery(name, []string{"id"}, columns), data).Scan(&encoded); err != nil {
		t.Fatal(err)
	}
	var plans []map[string]any
	if err = json.Unmarshal(encoded, &plans); err != nil {
		t.Fatal(err)
	}
	foundIndex := false
	var inspect func(map[string]any)
	inspect = func(plan map[string]any) {
		if plan["Relation Name"] == "history_bulk_plan" {
			if plan["Node Type"] == "Seq Scan" {
				t.Fatal("exact comparison scans large target per source row")
			}
			if plan["Node Type"] == "Index Scan" || plan["Node Type"] == "Index Only Scan" {
				condition, _ := plan["Index Cond"].(string)
				foundIndex = strings.Contains(condition, "id = e.id")
				t.Logf("target rows=100000 source rows=%d node=%s index=%s condition=%s actual_loops=%v actual_rows=%v shared_hit_blocks=%v", exactBulkRows, plan["Node Type"], plan["Index Name"], condition, plan["Actual Loops"], plan["Actual Rows"], plan["Shared Hit Blocks"])
			}
		}
		children, _ := plan["Plans"].([]any)
		for _, child := range children {
			inspect(child.(map[string]any))
		}
	}
	inspect(plans[0]["Plan"].(map[string]any))
	if !foundIndex {
		t.Fatalf("no key index condition in plan: %s", encoded)
	}
	if err = pgx.BeginTxFunc(ctx, target, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		matches, err := checkExactBulk(ctx, tx, "v3_audit", "history_bulk_plan", []string{"id"}, rows)
		if err != nil || len(matches) != exactBulkRows {
			t.Fatalf("bulk check rows=%d err=%v", len(matches), err)
		}
		for _, match := range matches {
			if !match {
				t.Fatal("large target comparison differs")
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Logf("large target exact check execution_ms=%v", plans[0]["Execution Time"])
}
