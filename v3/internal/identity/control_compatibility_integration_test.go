//go:build pgintegration

package identity

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
)

func identityHTTP(c *Control, method, path, token, version, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://codego.test"+path, strings.NewReader(body))
	r.Header.Set("Origin", "https://codego.test")
	r.Header.Set("X-CodeGo-API-Version", version)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	c.Handler().ServeHTTP(w, r)
	return w
}

func TestLegacyAndV3IdentityHTTPContracts(t *testing.T) {
	pool, _ := testDeps(t)
	c, err := NewControl(pool, ControlConfig{SessionSecret: bytes.Repeat([]byte{1}, 32), EncryptionKey: bytes.Repeat([]byte{2}, 32), PublicURL: "https://codego.test", BudgetPoster: ledger.NewPoster(pool)}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Register(ctx, RegisterInput{Username: "compat_user", Password: "strong-password"})
	if err != nil {
		t.Fatal(err)
	}
	login := identityHTTP(c, http.MethodPost, "/api/user/login", "", "", `{"username":"compat_user","password":"strong-password"}`)
	var old struct {
		Data struct {
			Role        int
			Status      int
			AccessToken string `json:"access_token"`
		}
	}
	if err = json.Unmarshal(login.Body.Bytes(), &old); err != nil || login.Code != 200 || old.Data.Role != 1 || old.Data.Status != 1 || old.Data.AccessToken == "" {
		t.Fatalf("old login=%s err=%v", login.Body, err)
	}
	session := old.Data.AccessToken
	modern := identityHTTP(c, http.MethodGet, "/api/user/self", session, "3", "")
	var current struct{ Data User }
	if err = json.Unmarshal(modern.Body.Bytes(), &current); err != nil || current.Data.Role != "user" {
		t.Fatalf("v3 profile=%s err=%v", modern.Body, err)
	}
	for _, name := range []string{"key-one", "key-two"} {
		created := identityHTTP(c, http.MethodPost, "/api/token/", session, "", `{"name":"`+name+`","unlimited_quota":false,"remain_quota":250,"model_limits_enabled":true,"model_limits":"model-x","allow_ips":"127.0.0.1","expired_time":-1}`)
		if created.Code != 200 {
			t.Fatalf("legacy create=%s", created.Body)
		}
	}
	listed := identityHTTP(c, http.MethodGet, "/api/token/?p=2&page_size=1", session, "", "")
	var page struct {
		Data struct {
			Page     int
			PageSize int `json:"page_size"`
			Total    int
			Items    []map[string]json.RawMessage
		}
	}
	if err = json.Unmarshal(listed.Body.Bytes(), &page); err != nil || listed.Code != 200 || page.Data.Page != 2 || page.Data.Total != 2 || len(page.Data.Items) != 1 {
		t.Fatalf("legacy list=%s err=%v", listed.Body, err)
	}
	item := page.Data.Items[0]
	if string(item["remain_quota"]) != "250" || string(item["model_limits_enabled"]) != "true" {
		t.Fatalf("legacy policy=%v", item)
	}
	item["status"] = json.RawMessage("2")
	body, _ := json.Marshal(item)
	updated := identityHTTP(c, http.MethodPut, "/api/token/", session, "", string(body))
	if updated.Code != 200 {
		t.Fatalf("old full-object update=%s", updated.Body)
	}
	modern = identityHTTP(c, http.MethodGet, "/api/token/", session, "3", "")
	var keys struct{ Data []KeyRecord }
	if err = json.Unmarshal(modern.Body.Bytes(), &keys); err != nil || len(keys.Data) != 2 {
		t.Fatalf("modern key list=%s err=%v", modern.Body, err)
	}
	for _, key := range keys.Data {
		if !key.BudgetLimited || *key.BudgetMicroCredits != 500 || key.UserID != u.ID || len(key.AllowedModels) != 1 {
			t.Fatalf("legacy edits lost policy %+v", key)
		}
	}
	var id int64
	if err = json.Unmarshal(item["id"], &id); err != nil {
		t.Fatal(err)
	}
	if err = c.UpdateKeyStatus(ctx, u.ID+1, id, "active"); err != ErrNotFound {
		t.Fatalf("foreign status edit accepted: %v", err)
	}
}

func TestEmailChangesRevokeVerificationFact(t *testing.T) {
	pool, _ := testDeps(t)
	c, _ := NewControl(pool, ControlConfig{SessionSecret: bytes.Repeat([]byte{1}, 32), EncryptionKey: bytes.Repeat([]byte{2}, 32)}, discardLogger())
	u, err := c.Register(ctx, RegisterInput{Username: "verified_user", Password: "strong-password", Email: "verified@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, pool, `UPDATE v3_identity.users SET email_verified=true WHERE id=$1`, u.ID)
	if _, err = c.UpdateProfile(ctx, u.ID, ProfileInput{Email: u.Email}); err != nil {
		t.Fatal(err)
	}
	var verified bool
	if err = pool.QueryRow(ctx, `SELECT email_verified FROM v3_identity.users WHERE id=$1`, u.ID).Scan(&verified); err != nil || !verified {
		t.Fatalf("unchanged verified email=%v %v", verified, err)
	}
	if _, err = c.UpdateProfile(ctx, u.ID, ProfileInput{Email: "different@example.test"}); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT email_verified FROM v3_identity.users WHERE id=$1`, u.ID).Scan(&verified); err != nil || verified {
		t.Fatalf("changed email kept proof=%v %v", verified, err)
	}
}
