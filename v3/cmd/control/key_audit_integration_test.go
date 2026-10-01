//go:build pgintegration

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/audit"
	"github.com/sh2001sh/new-api/v3/internal/identity"
)

type apiCall func(method, path, token, body string, status int) *httptest.ResponseRecorder

func verifyKeyAudit(t *testing.T, pool *pgxpool.Pool, call apiCall, cfg identity.ControlConfig, uid, accountID int64, session string) string {
	t.Helper()
	ctx := context.Background()
	w := call("POST", "/api/token/", session, `{"name":"finite-zero","budget_limited":true,"budget_micro_credits":0}`, 200)
	var budget struct {
		Data identity.KeyRecord `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &budget); err != nil || !budget.Data.BudgetLimited || budget.Data.BudgetAccountID <= 0 {
		t.Fatalf("finite key budget writer not wired: %v %s", err, w.Body)
	}
	id, err := identity.NewControl(pool, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	k, key, err := id.CreateKey(ctx, uid, identity.KeyInput{Name: "audit-key"})
	if err != nil {
		t.Fatal(err)
	}
	other, otherKey, err := id.CreateKey(ctx, uid, identity.KeyInput{Name: "other-key"})
	if err != nil {
		t.Fatal(err)
	}
	for i, keyID := range []int64{k.ID, other.ID} {
		if _, err := pool.Exec(ctx, `INSERT INTO v3_billing.usage_logs(created_at,account_id,user_id,key_id,amount,request_id,model) VALUES(now(),$1,$2,$3,0,$4,'control-model')`, accountID, uid, keyID, fmt.Sprintf("audit-key-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	w = call("GET", "/api/log/token", key, "", 200)
	var page struct {
		Data audit.Page `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Data.Items) != 1 || page.Data.Items[0].KeyID != k.ID {
		t.Fatalf("key leaked another key's usage: %v %s", err, w.Body)
	}
	call("GET", fmt.Sprintf("/api/log/token?key_id=%d", other.ID), key, "", 403)
	call("GET", fmt.Sprintf("/api/log/token?user_id=%d", uid+1), key, "", 403)
	call("GET", "/api/log/token", session, "", 401)
	call("GET", "/api/log/token?page_size=invalid", key, "", 400)
	for _, path := range []string{"/api/audit/events", "/api/audit/events/export", "/api/audit/requests"} {
		call("GET", path, key, "", 200)
		call("GET", path, session, "", 200)
		call("GET", fmt.Sprintf("%s?key_id=%d", path, other.ID), key, "", 403)
		call("GET", fmt.Sprintf("%s?user_id=%d", path, uid+1), key, "", 403)
	}
	call("GET", "/api/audit/requests/missing/attempts", key, "", 404)
	call("GET", "/api/audit/events?event_type=7", key, "", 400)
	for _, path := range []string{"/api/log/self", "/api/log/", "/api/audit/usage", "/api/catalog/channels", "/api/wallet", "/api/user/self"} {
		call("GET", path, key, "", 401)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_identity.api_keys SET allowed_cidrs=ARRAY['10.0.0.0/8']::cidr[] WHERE id=$1`, k.ID); err != nil {
		t.Fatal(err)
	}
	call("GET", "/api/log/token", key, "", 401)
	call("GET", "/api/audit/events", key, "", 401)
	r := httptest.NewRequest("GET", "/api/log/token", nil)
	r.Header.Set("Authorization", "Bearer "+key)
	r.Header.Set("X-Forwarded-For", "10.0.0.1")
	r.Header.Set("X-Real-IP", "10.0.0.1")
	if _, err := id.AuthenticateKeyRequest(r); !errors.Is(err, identity.ErrAddressNotAllowed) {
		t.Fatalf("untrusted forwarded address bypassed key CIDR: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_identity.api_keys SET allowed_cidrs=NULL,expires_at=$2 WHERE id=$1`, k.ID, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	call("GET", "/api/log/token", key, "", 401)
	if _, err := pool.Exec(ctx, `UPDATE v3_identity.api_keys SET expires_at=NULL,status='disabled' WHERE id=$1`, k.ID); err != nil {
		t.Fatal(err)
	}
	call("GET", "/api/log/token", key, "", 401)
	return otherKey
}
