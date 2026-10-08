//go:build pgintegration

package migrations

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestOrphanAttemptHistoryPreservesSourceAndLiveForeignKey(t *testing.T) {
	dsn := os.Getenv("V3_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("V3_TEST_PG_DSN not set; use a disposable schema test database")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadWrite})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Build a new disposable database in this transaction, or use the schema
	// already installed by the existing serial CI migration tests. Never drop
	// schemas or leave this test's records behind.
	var hasLive, hasHistory bool
	if err := tx.QueryRow(ctx, `SELECT
		to_regclass('v3_audit.request_attempt_audits') IS NOT NULL,
		to_regclass('v3_audit.orphan_request_attempt_history') IS NOT NULL`).Scan(&hasLive, &hasHistory); err != nil {
		t.Fatal(err)
	}
	if !hasLive {
		names, err := Files()
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range names {
			sql, err := Read(name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, sql); err != nil {
				t.Fatalf("apply %s: %v", name, err)
			}
		}
	} else if !hasHistory {
		sql, err := Read("20261009000106_orphan_attempt_history.sql")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	const source = `{"attempt_id":"old-attempt","request_id":"missing-request","attempt_no":2,"status":"failed","source_extra":{"nullable":null,"values":[1,true,"original"]},"started_at":"0001-01-01T08:05:43+08:05:43"}`
	if _, err := tx.Exec(ctx, `INSERT INTO v3_audit.orphan_request_attempt_history
		(attempt_id,request_id,source_record) VALUES ($1,$2,$3::jsonb)`, "old-attempt", "missing-request", source); err != nil {
		t.Fatal(err)
	}
	var exact bool
	var fabricatedParents int
	if err := tx.QueryRow(ctx, `SELECT source_record=$1::jsonb,
		(SELECT count(*) FROM v3_audit.request_audits WHERE request_id='missing-request')
		FROM v3_audit.orphan_request_attempt_history WHERE attempt_id='old-attempt'`, source).Scan(&exact, &fabricatedParents); err != nil || !exact || fabricatedParents != 0 {
		t.Fatalf("original JSON retained=%v, fabricated parents=%d, err=%v", exact, fabricatedParents, err)
	}

	cases := []struct {
		name, attemptID, requestID string
		record                     any
		code                       string
	}{
		{"duplicate attempt", "old-attempt", "missing-request", source, "23505"},
		{"empty attempt ID", "", "missing-request", `{"attempt_id":"","request_id":"missing-request"}`, "23514"},
		{"empty request ID", "new-attempt", "", `{"attempt_id":"new-attempt","request_id":""}`, "23514"},
		{"different attempt ID", "new-attempt", "missing-request", `{"attempt_id":"other-attempt","request_id":"missing-request"}`, "23514"},
		{"different request ID", "new-attempt", "missing-request", `{"attempt_id":"new-attempt","request_id":"other-request"}`, "23514"},
		{"missing attempt ID", "new-attempt", "missing-request", `{"request_id":"missing-request"}`, "23514"},
		{"missing request ID", "new-attempt", "missing-request", `{"attempt_id":"new-attempt"}`, "23514"},
		{"JSON null attempt ID", "new-attempt", "missing-request", `{"attempt_id":null,"request_id":"missing-request"}`, "23514"},
		{"JSON null request ID", "new-attempt", "missing-request", `{"attempt_id":"new-attempt","request_id":null}`, "23514"},
		{"JSON null record", "new-attempt", "missing-request", `null`, "23514"},
		{"array record", "new-attempt", "missing-request", `[]`, "23514"},
		{"string record", "new-attempt", "missing-request", `"not an object"`, "23514"},
		{"number record", "new-attempt", "missing-request", `1`, "23514"},
		{"boolean record", "new-attempt", "missing-request", `true`, "23514"},
		{"SQL null record", "new-attempt", "missing-request", nil, "23502"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tx.Exec(ctx, "SAVEPOINT reject_history"); err != nil {
				t.Fatal(err)
			}
			_, err := tx.Exec(ctx, `INSERT INTO v3_audit.orphan_request_attempt_history
				(attempt_id,request_id,source_record) VALUES ($1,$2,$3::jsonb)`, tc.attemptID, tc.requestID, tc.record)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != tc.code {
				t.Fatalf("SQLSTATE=%v, want %s", err, tc.code)
			}
			if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT reject_history"); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, "RELEASE SAVEPOINT reject_history"); err != nil {
				t.Fatal(err)
			}
		})
	}

	// The original live table must still reject this exact missing parent even
	// though its historical source record is safely retained in the new table.
	if _, err := tx.Exec(ctx, "SAVEPOINT reject_live_attempt"); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO v3_audit.request_attempt_audits
		(attempt_id,request_id,attempt_no,retry_index,channel_id,model,fault_domain,
		request_type,status,success,status_code,failure_class,stage,started_at,
		completed_at,duration_ms,created_at)
		VALUES ('old-attempt','missing-request',0,0,0,'model','provider','sync',
		'failed',false,500,'provider','completed',now(),now(),0,now())`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Fatalf("native live audit FK: %v, want SQLSTATE 23503", err)
	}
	if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT reject_live_attempt"); err != nil {
		t.Fatal(err)
	}
	var rows, foreignKeys, indexes int
	if err := tx.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM v3_audit.orphan_request_attempt_history WHERE attempt_id='old-attempt'),
		(SELECT count(*) FROM pg_constraint WHERE conrelid='v3_audit.orphan_request_attempt_history'::regclass AND contype='f'),
		(SELECT count(*) FROM pg_index WHERE indrelid='v3_audit.orphan_request_attempt_history'::regclass)
		`).Scan(&rows, &foreignKeys, &indexes); err != nil || rows != 1 || foreignKeys != 0 || indexes != 1 {
		t.Fatalf("history rows=%d foreign keys=%d indexes=%d err=%v", rows, foreignKeys, indexes, err)
	}
}
