//go:build pgintegration

package legacy

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestHistoryCachedEncodingExactPrecisionReplayAndRollback(t *testing.T) {
	_, target, _ := importTestDB(t)
	ctx := context.Background()
	if _, err := target.Exec(ctx, `CREATE TABLE v3_audit.cached_encoding(id bigint PRIMARY KEY,amount bigint NOT NULL,metadata jsonb NOT NULL,ciphertext bytea)`); err != nil {
		t.Fatal(err)
	}
	row := map[string]any{"id": int64(9007199254740993), "amount": int64(-9223372036854775807), "metadata": json.RawMessage(`{"precise":9007199254740993,"html":"<"}`), "ciphertext": []byte{0, 1, 255}}
	for i := 0; i < 2; i++ {
		if err := pgx.BeginFunc(ctx, target, func(tx pgx.Tx) error {
			batch := historyImportBatch(ctx, tx, "v3_audit", "cached_encoding", "id")
			if err := batch.add(row); err != nil {
				return err
			}
			return batch.finish()
		}); err != nil {
			t.Fatal(err)
		}
	}
	var amount int64
	var ciphertext []byte
	var precise string
	if err := target.QueryRow(ctx, `SELECT amount,ciphertext,metadata->>'precise' FROM v3_audit.cached_encoding`).Scan(&amount, &ciphertext, &precise); err != nil || amount != -9223372036854775807 || precise != "9007199254740993" || !reflect.DeepEqual(ciphertext, []byte{0, 1, 255}) {
		t.Fatalf("cached precision amount=%d binary=%v precise=%s err=%v", amount, ciphertext, precise, err)
	}
	err := pgx.BeginFunc(ctx, target, func(tx pgx.Tx) error {
		batch := historyImportBatch(ctx, tx, "v3_audit", "cached_encoding", "id")
		if err := batch.add(map[string]any{"id": int64(2), "amount": int64(3), "metadata": json.RawMessage(`{}`), "ciphertext": nil}); err != nil {
			return err
		}
		if err := batch.finish(); err != nil {
			return err
		}
		changed := map[string]any{"id": row["id"], "amount": int64(7), "metadata": row["metadata"], "ciphertext": row["ciphertext"]}
		if err := batch.add(changed); err != nil {
			return err
		}
		return batch.finish()
	})
	if err == nil || !strings.Contains(err.Error(), "conflicts with target") {
		t.Fatalf("cached conflict accepted: %v", err)
	}
	var count int
	if err := target.QueryRow(ctx, `SELECT count(*) FROM v3_audit.cached_encoding`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("cached earlier batch escaped rollback count=%d err=%v", count, err)
	}
	// Wrong source cardinality must refuse the transaction, even if every row
	// returned by the exact comparison matches.
	err = pgx.BeginFunc(ctx, target, func(tx pgx.Tx) error {
		rows := []map[string]any{{"id": int64(4), "amount": int64(5), "metadata": json.RawMessage(`{}`), "ciphertext": nil}}
		name, columns, data, err := exactBulkInput("v3_audit", "cached_encoding", []string{"id"}, rows)
		if err != nil {
			return err
		}
		return insertExactBulkData(ctx, tx, name, "cached_encoding", []string{"id"}, columns, data, 2)
	})
	if err == nil || !strings.Contains(err.Error(), "wrong row count") {
		t.Fatalf("cardinality mismatch accepted: %v", err)
	}
	if err := target.QueryRow(ctx, `SELECT count(*) FROM v3_audit.cached_encoding`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("wrong count escaped rollback count=%d err=%v", count, err)
	}
}

func TestOnlineDeleteMetricProjectionCoversEveryNumericStageColumn(t *testing.T) {
	_, target, _ := importTestDB(t)
	ctx := context.Background()
	sources := map[string]string{}
	for _, name := range []string{"ledger_entries", "logs", "request_audits", "request_attempt_audits", "funding_lots", "funding_allocations"} {
		sources[name] = "test"
	}
	for _, name := range channelMarketSourceTables {
		sources["marketplace_"+name] = "test"
	}
	fields := map[string]any{"owner_user_id": int64(9007199254740993), "status": "pending"}
	for _, field := range onlineMetricFields {
		fields[field] = int64(9007199254740993)
	}
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range onlineSortedTables(onlineSpecs(sources)) {
		name := pgx.Identifier(strings.Split(table, ".")).Sanitize()
		rows, err := target.Query(ctx, "SELECT to_jsonb(h),"+onlineDeleteMetrics(table)+" FROM jsonb_populate_record(NULL::"+name+",$1::jsonb) h", data)
		if err != nil {
			t.Fatalf("table %s: %v", table, err)
		}
		for rows.Next() {
			var full, projected []byte
			if err := rows.Scan(&full, &projected); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			a, b := onlineMetrics{}, onlineMetrics{}
			if err := a.row(table, full, -1); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			if err := b.row(table, projected, -1); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			if !reflect.DeepEqual(a, b) {
				rows.Close()
				t.Fatalf("delete metrics differ table=%s full=%v projected=%v", table, a, b)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
	}
}
