//go:build pgintegration

package audit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/migrations"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("V3_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("V3_TEST_PG_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	rows, err := pool.Query(ctx, `SELECT schema_name FROM information_schema.schemata WHERE left(schema_name,3)='v3_'`)
	if err != nil {
		t.Fatal(err)
	}
	var schemas []string
	for rows.Next() {
		var schema string
		if err := rows.Scan(&schema); err != nil {
			t.Fatal(err)
		}
		schemas = append(schemas, schema)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, schema := range schemas {
		if _, err := pool.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Fatal(err)
		}
	}
	names, err := migrations.Files()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		sql, err := migrations.Read(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	return pool
}

func TestUsageCursorOwnershipExportAndSamplePersistence(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO v3_identity.users(id,username) VALUES(1,'alice'),(2,'bob');
 INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',1,'wallet'),('user',2,'wallet');`)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 30, 1, 2, 3, 0, time.UTC)
	for _, row := range []struct {
		user, key, amount int64
		request, model    string
	}{{1, 10, 11, "r1", "model"}, {1, 10, 22, "r2", "model"}, {1, 11, 33, "r3", "=malicious"}, {2, 20, 99, "other", "model"}} {
		_, err := pool.Exec(ctx, `INSERT INTO v3_billing.usage_logs(created_at,account_id,user_id,key_id,amount,request_id,model,prompt_tokens,completion_tokens)
 VALUES($1,$2,$2,$3,$4,$5,$6,10,5)`, at, row.user, row.key, row.amount, row.request, row.model)
		if err != nil {
			t.Fatal(err)
		}
	}
	s := New(pool, Config{SampleRatePPM: 1_000_000, Authenticate: func(*http.Request) (Principal, error) { return Principal{UserID: 1}, nil }})
	page, err := s.List(ctx, Principal{UserID: 1}, Query{Limit: 2})
	if err != nil || len(page.Items) != 2 || !page.HasMore {
		t.Fatalf("first page %+v %v", page, err)
	}
	next, err := s.List(ctx, Principal{UserID: 1}, Query{Limit: 2, Cursor: page.NextCursor})
	if err != nil || len(next.Items) != 1 || next.HasMore || next.Items[0].ID >= page.Items[1].ID {
		t.Fatalf("cursor page %+v %v", next, err)
	}
	if _, err := s.List(ctx, Principal{UserID: 1}, Query{UserID: 2}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("ownership not enforced %v", err)
	}
	keyPage, err := s.List(ctx, Principal{UserID: 1, KeyID: 10}, Query{})
	if err != nil || len(keyPage.Items) != 2 {
		t.Fatalf("key page %+v %v", keyPage, err)
	}
	summary, err := s.Summarize(ctx, Principal{UserID: 1}, Query{})
	if err != nil || summary.Requests != 3 || summary.Amount != 66 {
		t.Fatalf("summary %+v %v", summary, err)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/log/self/export", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "'=malicious") || strings.Contains(w.Body.String(), "other") {
		t.Fatalf("export %d %s", w.Code, w.Body.String())
	}
	sample := Sample{RequestID: "r1", UserID: 1, Model: "model", CreatedAt: at, Request: json.RawMessage(`{"api_key":"secret","messages":[]}`), Response: json.RawMessage(`{"ok":true}`)}
	for i := 0; i < 2; i++ {
		if err := s.RecordSample(ctx, sample); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	var body string
	if err := pool.QueryRow(ctx, `SELECT count(*),max(request_body::text) FROM v3_audit.request_samples`).Scan(&count, &body); err != nil {
		t.Fatal(err)
	}
	if count != 1 || strings.Contains(body, "secret") {
		t.Fatalf("sample persisted %d %s", count, body)
	}
	if n, err := s.DeleteSamplesBefore(ctx, at.Add(time.Second)); err != nil || n != 1 {
		t.Fatalf("retention %d %v", n, err)
	}
}
