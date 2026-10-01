//go:build pgintegration

package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/identity"
	"github.com/sh2001sh/new-api/v3/internal/security"
)

func verifySecurityAuditAssembly(t *testing.T, pool *pgxpool.Pool, call apiCall, admin string, adminID int64, apiKey string) {
	t.Helper()
	w := call("POST", "/api/user/register", "", `{"username":"security_owner","password":"strong-password"}`, 200)
	var registered struct {
		Data identity.Session `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &registered); err != nil || registered.Data.AccessToken == "" {
		t.Fatalf("security owner registration: %v", err)
	}
	owner := registered.Data.AccessToken
	if _, err := pool.Exec(context.Background(), `INSERT INTO v3_security.security_audit_events
	(id,dedupe_key,request_id,source,decision,risk_code,severity,user_id,owner_user_id,marketplace_channel_id,upstream_error_body,upstream_error_message,prompt_preview,review_status,created_at)
	VALUES('control-admin-event','control-admin-dedupe','admin-request','prompt_guard','blocked','risk','high',1,$1,'admin-channel','PRIVATE ADMIN BODY','PRIVATE ADMIN MESSAGE','PRIVATE ADMIN PROMPT','unreviewed',now()),
	('control-owner-event','control-owner-dedupe','owner-request','prompt_guard','blocked','=RISK()','high',9007199254740993,$2,'owner-channel','PRIVATE OWNER BODY','PRIVATE OWNER MESSAGE','PRIVATE OWNER PROMPT','unreviewed',now())`, adminID, registered.Data.User.ID); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/security-audit/events", "/api/marketplace/security-audit/events"} {
		call("GET", path, "", "", 401)
		call("GET", path, apiKey, "", 401)
	}
	call("GET", "/api/marketplace/admin/security-audit/events", owner, "", 403)
	list := func(path, token string, wrapped bool) security.EventList {
		t.Helper()
		response := call("GET", path, token, "", 200)
		var result security.EventList
		if wrapped {
			var envelope struct {
				Success bool               `json:"success"`
				Data    security.EventList `json:"data"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || !envelope.Success {
				t.Fatalf("security alias response: %v", err)
			}
			result = envelope.Data
		} else if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	for _, path := range []string{"/api/security-audit/events?page_size=20", "/api/marketplace/security-audit/events?channel_id=owner-channel"} {
		result := list(path, owner, strings.Contains(path, "/marketplace/"))
		if result.Total != 1 || len(result.Items) != 1 || result.Items[0].ID != "control-owner-event" || result.Items[0].UserID != 9007199254740993 || result.Items[0].PromptPreview != "" || result.Items[0].UpstreamErrorBody != "" || result.Items[0].UpstreamErrorMessage != "" {
			t.Fatal("owner audit lost original identity or exposed privileged evidence")
		}
	}
	// Admin privileges must not expand the retained owner route.
	result := list("/api/marketplace/security-audit/events", admin, true)
	if result.Total != 1 || result.PageSize != 20 || result.Items[0].ID != "control-admin-event" || result.Items[0].UpstreamErrorBody != "" {
		t.Fatal("admin session escaped owner alias or lost original pagination")
	}
	for _, path := range []string{"/api/security-audit/events?page_size=20", "/api/marketplace/admin/security-audit/events"} {
		result := list(path, admin, strings.Contains(path, "/marketplace/"))
		if result.Total != 2 || len(result.Items) != 2 || result.Items[0].UpstreamErrorBody == "" {
			t.Fatal("administrator cannot read global retained security evidence")
		}
	}
	for _, id := range []string{"control-admin-event", "missing-string-event"} {
		call("GET", "/api/security-audit/events/"+id, owner, "", 404)
		call("PATCH", "/api/marketplace/security-audit/events/"+id, owner, `{"status":"resolved"}`, 404)
	}
	call("PATCH", "/api/security-audit/events/control-owner-event", owner, `{`, 400)
	call("PATCH", "/api/security-audit/events/control-owner-event", owner, `{"status":"invalid"}`, 400)
	w = call("PATCH", "/api/security-audit/events/control-owner-event", owner, `{"status":"resolved","note":"owner review"}`, 200)
	var reviewed security.Event
	if err := json.Unmarshal(w.Body.Bytes(), &reviewed); err != nil || reviewed.ID != "control-owner-event" || reviewed.ReviewStatus != "resolved" || reviewed.ReviewNote != "owner review" || reviewed.ReviewedBy != registered.Data.User.ID || reviewed.ReviewedAt == nil || reviewed.UpstreamErrorBody != "" {
		t.Fatalf("canonical protected string-ID review: %v", err)
	}
	w = call("PATCH", "/api/marketplace/admin/security-audit/events/control-owner-event", admin, `{"status":"acknowledged","note":"admin review"}`, 200)
	var envelope struct {
		Success bool           `json:"success"`
		Data    security.Event `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil || !envelope.Success || envelope.Data.ReviewedBy != adminID || envelope.Data.ReviewStatus != "acknowledged" || envelope.Data.ReviewNote != "admin review" || envelope.Data.UpstreamErrorBody == "" {
		t.Fatalf("admin alias string-ID review: %v", err)
	}
	w = call("GET", "/api/marketplace/security-audit/events/export?channel_id=owner-channel", owner, "", 200)
	rows, err := csv.NewReader(w.Body).ReadAll()
	if err != nil || len(rows) != 2 || len(rows[1]) != 35 || rows[1][0] != "control-owner-event" || rows[1][4] != "'=RISK()" || rows[1][6] != "9007199254740993" || rows[1][18] != "" || rows[1][19] != "" || rows[1][21] != "" {
		t.Fatalf("owner alias CSV privacy/identity/formula contract failed: %v", err)
	}
	w = call("GET", "/api/security-audit/events/export", owner, "", 200)
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/csv") || strings.Contains(w.Body.String(), "PRIVATE") || strings.Contains(w.Body.String(), "admin-request") {
		t.Fatal("canonical owner export lost scope or disclosed privileged evidence")
	}
	rows, err = csv.NewReader(w.Body).ReadAll()
	if err != nil || len(rows) != 2 || rows[1][4] != "'=RISK()" {
		t.Fatalf("canonical security CSV did not protect formula cells: %v", err)
	}
}
