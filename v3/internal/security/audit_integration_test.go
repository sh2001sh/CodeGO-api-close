//go:build pgintegration

package security

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testAuditAccess(t *testing.T, g *Guard, pool *pgxpool.Pool) {
	t.Helper()
	t.Run("audit owner scope foreign review privacy exact CSV", func(t *testing.T) {
		ctx := context.Background()
		_, err := pool.Exec(ctx, `INSERT INTO v3_security.security_audit_events(id,dedupe_key,request_id,source,decision,risk_code,severity,user_id,owner_user_id,upstream_error_body,upstream_error_message,prompt_preview,review_status,created_at,updated_at) VALUES('original-string-id','dedupe1','old-request','prompt_guard','blocked','risk','high',9007199254740993,10,'secret upstream credential','secret message','secret imported preview','unreviewed','2025-01-01 01:02:03.123456+00',NULL),('foreign-id','dedupe2','foreign-request','prompt_guard','blocked','risk','high',10,11,'FOREIGN SECRET','FOREIGN MESSAGE','FOREIGN PROMPT','unreviewed',now(),now())`)
		if err != nil {
			t.Fatal(err)
		}
		owner := Actor{UserID: 10}
		admin := Actor{UserID: 99, Admin: true}
		list, err := g.List(ctx, owner, Query{PageSize: 100})
		if err != nil || list.Total != 1 || len(list.Items) != 1 {
			t.Fatalf("scoped list %+v %v", list, err)
		}
		event := list.Items[0]
		if event.ID != "original-string-id" || event.UserID != 9007199254740993 || event.UpdatedAt != nil || event.UpstreamErrorBody != "" || event.PromptPreview != "" || event.UpstreamErrorMessage != "" {
			t.Fatalf("event redaction or field loss %+v", event)
		}
		if _, err = g.Get(ctx, owner, "foreign-id"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("foreign read %v", err)
		}
		if _, err = g.Review(ctx, owner, "foreign-id", "resolved", "steal"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("foreign review %v", err)
		}
		if _, err = g.Review(ctx, owner, event.ID, "invalid", "note"); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid status accepted")
		}
		reviewed, err := g.Review(ctx, owner, event.ID, "resolved", "reviewed note")
		if err != nil || reviewed.ReviewedBy != 10 || reviewed.ReviewedAt == nil {
			t.Fatalf("review %+v %v", reviewed, err)
		}
		reviewed, err = g.Review(ctx, owner, event.ID, "unreviewed", "reset")
		if err != nil || reviewed.ReviewedBy != 0 || reviewed.ReviewedAt != nil {
			t.Fatal("unreviewed metadata retained")
		}
		event, err = g.Get(ctx, admin, event.ID)
		if err != nil || event.UpstreamErrorBody != "secret upstream credential" || event.PromptPreview != "secret imported preview" {
			t.Fatal("privileged evidence was lost")
		}
		var out bytes.Buffer
		if err = g.ExportCSV(ctx, owner, Query{}, &out); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "secret") || strings.Contains(out.String(), "FOREIGN") {
			t.Fatal("sensitive audit data escaped owner export")
		}
		rows, err := csv.NewReader(&out).ReadAll()
		if err != nil || len(rows) != 2 || len(rows[0]) != 35 || rows[1][6] != "9007199254740993" || rows[1][0] != "original-string-id" {
			t.Fatalf("CSV exact fields %v %v", rows, err)
		}
		mux := http.NewServeMux()
		g.Register(mux, func(*http.Request) (Actor, error) { return owner, nil })
		req := httptest.NewRequest("GET", "http://example.test/api/security-audit/events/foreign-id", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != 404 {
			t.Fatalf("HTTP foreign %d", w.Code)
		}
		req = httptest.NewRequest("PATCH", "http://example.test/api/security-audit/events/original-string-id", strings.NewReader(`{"status":"resolved"}`))
		req.Header.Set("Origin", "https://foreign.test")
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != 403 {
			t.Fatalf("cross-site review %d", w.Code)
		}
		mux = http.NewServeMux()
		g.Register(mux, nil)
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "http://example.test/api/security-audit/events", nil))
		if w.Code != 401 {
			t.Fatal("anonymous audit permitted")
		}
	})
}
