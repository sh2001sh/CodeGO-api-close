//go:build pgintegration

package channelmarket_test

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
	"github.com/sh2001sh/new-api/v3/internal/security"
)

func TestGlobalSecurityAliasesComposeOriginalStringHistoryAndEnforceScope(t *testing.T) {
	f := setup(t)
	_, err := f.pool.Exec(ctx, `INSERT INTO v3_security.security_audit_events
	(id,dedupe_key,request_id,source,decision,risk_code,severity,user_id,owner_user_id,marketplace_channel_id,model,upstream_error_body,upstream_error_message,prompt_preview,review_status,created_at)
	VALUES('original-string-id','original-dedupe','original-request','prompt_guard','blocked','=RISK()','high',9007199254740993,1,'legacy-channel','fixture-model','OWNER SECRET','OWNER MESSAGE','OWNER PROMPT','unreviewed','2025-01-01'),
	('foreign-string-id','foreign-dedupe','FOREIGN-request','prompt_guard','blocked','foreign','high',2,2,'foreign-channel','fixture-model','FOREIGN SECRET','FOREIGN MESSAGE','FOREIGN PROMPT','unreviewed',now())`)
	if err != nil {
		t.Fatal(err)
	}
	guard, err := security.New(f.pool, nil, security.Config{})
	if err != nil {
		t.Fatal(err)
	}
	current := channelmarket.Actor{UserID: 1}
	handler := guard.Handler(func(r *http.Request) (security.Actor, error) {
		return security.Actor{UserID: current.UserID, Admin: current.Admin && strings.Contains(r.URL.Path, "/admin/")}, nil
	})
	service := channelmarket.New(f.pool, nil, nil, channelmarket.Config{SecurityAuditHandler: handler}, nil)
	mux := http.NewServeMux()
	service.Register(mux, func(*http.Request) (channelmarket.Actor, error) { return current, nil })
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		return w
	}
	list := func(path string) security.EventList {
		t.Helper()
		w := request(http.MethodGet, path, "")
		var result struct {
			Success bool               `json:"success"`
			Data    security.EventList `json:"data"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || !result.Success {
			t.Fatalf("legacy list response %d %s", w.Code, w.Body.String())
		}
		return result.Data
	}
	owner := list("/api/marketplace/security-audit/events?channel_id=legacy-channel")
	if owner.Total != 1 || owner.Page != 1 || owner.PageSize != 20 || len(owner.Items) != 1 || owner.Items[0].ID != "original-string-id" ||
		owner.Items[0].UserID != 9007199254740993 || owner.Items[0].UpstreamErrorBody != "" || owner.Items[0].PromptPreview != "" || owner.Items[0].UpstreamErrorMessage != "" {
		t.Fatalf("original string history/scoped redaction lost: %+v", owner)
	}
	filtered := list("/api/marketplace/security-audit/events?channel_id=foreign-channel")
	if filtered.Total != 0 || len(filtered.Items) != 0 {
		t.Fatal("channel filter escaped owner scope")
	}
	for _, path := range []string{"/api/marketplace/admin/security-audit/events", "/api/marketplace/admin/security-audit/events/export"} {
		if w := request(http.MethodGet, path, ""); w.Code != 403 {
			t.Fatalf("non-admin reached global path %s: %d", path, w.Code)
		}
	}
	for _, id := range []string{"foreign-string-id", "missing-string-id"} {
		w := request(http.MethodPatch, "/api/marketplace/security-audit/events/"+id, `{"status":"resolved","note":"denied"}`)
		if w.Code != 404 || !strings.Contains(w.Body.String(), `"success":false`) {
			t.Fatalf("foreign/unknown review response: %d %s", w.Code, w.Body.String())
		}
	}
	w := request(http.MethodPatch, "/api/marketplace/security-audit/events/original-string-id", `{"status":"resolved","note":"retained original note"}`)
	var reviewed struct {
		Success bool           `json:"success"`
		Data    security.Event `json:"data"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &reviewed) != nil || !reviewed.Success || reviewed.Data.ID != "original-string-id" || reviewed.Data.ReviewStatus != "resolved" ||
		reviewed.Data.ReviewNote != "retained original note" || reviewed.Data.ReviewedBy != 1 || reviewed.Data.ReviewedAt == nil {
		t.Fatalf("original string review contract lost: %d %s", w.Code, w.Body.String())
	}
	w = request(http.MethodGet, "/api/marketplace/security-audit/events/export?channel_id=legacy-channel", "")
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "text/csv") || strings.Contains(w.Body.String(), "SECRET") || strings.Contains(w.Body.String(), "FOREIGN") || strings.Contains(w.Body.String(), "OWNER PROMPT") {
		t.Fatalf("owner export leaked foreign/private evidence: %d %s", w.Code, w.Body.String())
	}
	rows, err := csv.NewReader(w.Body).ReadAll()
	if err != nil || len(rows) != 2 || len(rows[1]) != len(rows[0]) {
		t.Fatalf("CSV rows malformed: %v %v", rows, err)
	}
	columns := map[string]int{}
	for index, name := range rows[0] {
		columns[name] = index
	}
	for name, expected := range map[string]string{"id": "original-string-id", "risk_code": "'=RISK()", "user_id": "9007199254740993"} {
		index, exists := columns[name]
		if !exists || rows[1][index] != expected {
			t.Fatalf("CSV field %s changed: header=%v row=%v", name, rows[0], rows[1])
		}
	}
	current = channelmarket.Actor{UserID: 3, Admin: true}
	admin := list("/api/marketplace/admin/security-audit/events")
	if admin.Total != 2 || len(admin.Items) != 2 || admin.Items[0].UpstreamErrorBody == "" {
		t.Fatal("admin lost retained original global evidence")
	}
	// The original owner alias remains owner-scoped for an administrator too.
	if own := list("/api/marketplace/security-audit/events"); own.Total != 0 {
		t.Fatal("admin session broadened the original owner alias")
	}
	w = request(http.MethodPatch, "/api/marketplace/admin/security-audit/events/foreign-string-id", `{"status":"acknowledged","note":"admin note"}`)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &reviewed) != nil || reviewed.Data.ReviewedBy != 3 || reviewed.Data.ReviewNote != "admin note" {
		t.Fatalf("admin string review failed: %d %s", w.Code, w.Body.String())
	}
	current = channelmarket.Actor{UserID: 1}
	if _, err := f.pool.Exec(ctx, `INSERT INTO v3_security.security_audit_events(id,dedupe_key,source,decision,risk_code,severity,owner_user_id,review_status,created_at)
	SELECT 'boundary-'||i,'boundary-dedupe-'||i,'prompt_guard','blocked','risk','high',1,'unreviewed',now() FROM generate_series(1,19999) i`); err != nil {
		t.Fatal(err)
	}
	w = request(http.MethodGet, "/api/marketplace/security-audit/events/export", "")
	if w.Code != 200 {
		t.Fatalf("20000-row inclusive export boundary rejected: %d %s", w.Code, w.Body.String())
	}
	rows, err = csv.NewReader(w.Body).ReadAll()
	if err != nil || len(rows) != 20001 {
		t.Fatalf("export silently truncated: rows=%d err=%v", len(rows), err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO v3_security.security_audit_events(id,dedupe_key,source,decision,risk_code,severity,owner_user_id,review_status) VALUES('overflow','overflow','prompt_guard','blocked','risk','high',1,'unreviewed')`); err != nil {
		t.Fatal(err)
	}
	w = request(http.MethodGet, "/api/marketplace/security-audit/events/export", "")
	if w.Code != 400 || !strings.Contains(w.Body.String(), `"success":false`) || w.Header().Get("Content-Disposition") != "" || !strings.Contains(w.Body.String(), "20000") {
		t.Fatalf("export overflow fabricated partial success: %d %s", w.Code, w.Body.String())
	}
}
