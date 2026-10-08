//go:build pgintegration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/cmd/internal/boot"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/identity"
	"github.com/sh2001sh/new-api/v3/migrations"
	"github.com/sh2001sh/new-api/v3/pkg/pg"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

// This suite uses the same disposable integration services as verify.sh.
func TestComposedControlAPI(t *testing.T) {
	dsn, addr := os.Getenv("V3_TEST_PG_DSN"), os.Getenv("V3_TEST_REDIS_ADDR")
	if dsn == "" || addr == "" {
		t.Skip("V3_TEST_PG_DSN / V3_TEST_REDIS_ADDR not set")
	}
	ctx := context.Background()
	clearComposedPaymentEnvironment(t)
	pool, err := pg.Connect(ctx, pg.Config{DSN: dsn, MaxConns: 5})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	rows, err := pool.Query(ctx, `SELECT nspname FROM pg_namespace WHERE left(nspname,3)='v3_' ORDER BY nspname`)
	if err != nil {
		t.Fatal(err)
	}
	schemas, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	for _, schema := range schemas {
		if _, err := pool.Exec(ctx, "DROP SCHEMA IF EXISTS "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
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
			t.Fatalf("migration %s: %v", name, err)
		}
	}
	rdb, err := redisx.Connect(redisx.Config{Addr: addr})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rdb.Close() })
	key := bytes.Repeat([]byte{3}, 32)
	crypto, err := catalog.NewAESGCM(key)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config{Identity: identity.ControlConfig{PublicURL: "http://control.test", SessionSecret: bytes.Repeat([]byte{5}, 32), EncryptionKey: key}, OIDC: testOIDCConfig(t)}
	badCfg := cfg
	badCfg.InternalGatewayURL = "http://user:sensitive-value@gateway.test"
	if _, err := controlHandler(&boot.Deps{PG: pool, Redis: rdb, Crypto: crypto}, badCfg, "", slog.New(slog.NewTextHandler(io.Discard, nil))); err == nil || strings.Contains(err.Error(), "sensitive-value") {
		t.Fatalf("unsafe batch gateway configuration accepted or disclosed: %v", err)
	}
	assets := t.TempDir()
	if err := os.WriteFile(filepath.Join(assets, "index.html"), []byte("application"), 0600); err != nil {
		t.Fatal(err)
	}
	h, err := controlHandler(&boot.Deps{PG: pool, Redis: rdb, Crypto: crypto}, cfg, assets, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path, token, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "http://control.test"+path, strings.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CodeGo-API-Version", "3")
		r.Header.Set("Origin", "http://control.test")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: status=%d want=%d body=%s", method, path, w.Code, status, w.Body)
		}
		return w
	}
	call("GET", "/readyz", "", "", 200)
	call("GET", "/api/status", "", "", 200)
	call("GET", "/api/catalog/channels", "", "", 401)
	w := call("POST", "/api/user/register", "", `{"username":"control_alice","password":"strong-password","accepted_terms_version":"2026-10-07","accepted_privacy_version":"2026-10-07","agreement_locale":"en"}`, 200)
	var registration struct {
		Data identity.Session `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &registration); err != nil || registration.Data.AccessToken == "" {
		t.Fatalf("registration: %v %s", err, w.Body)
	}
	token := registration.Data.AccessToken
	call("GET", "/api/wallet/refunds/eligible", "", "", 401)
	call("GET", "/api/wallet/refunds/eligible", token, "", 200)
	call("POST", "/api/wallet/refunds", token, `{`, 400)
	call("POST", "/api/wallet/refunds", token, `{"order_type":"balance","trade_no":"missing"}`, 503)
	call("POST", "/api/wallet/transfers/payment-password/email-code", token, `{}`, 503)
	call("GET", "/api/catalog/channels", token, "", 403)
	call("GET", "/api/catalog/models", token, "", 403)
	call("POST", "/api/billing/adjustments", token, `{"account_id":1,"amount_micro":1,"operation_id":"a","reason":"test"}`, 403)
	w = call("GET", "/api/wallet", token, "", 200)
	var wallet struct {
		Data struct {
			AccountID int64  `json:"account_id"`
			Balance   string `json:"balance_micro_credits"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &wallet); err != nil || wallet.Data.AccountID <= 0 || wallet.Data.Balance != "0" {
		t.Fatalf("wallet=%s err=%v", w.Body, err)
	}
	call("GET", "/api/log/self", token, "", 200)
	call("GET", "/api/billing/history", "", "", 401)
	call("GET", "/api/billing/history", token, "", 200)
	call("GET", "/api/billing/history?limit=201", token, "", 400)
	call("GET", "/api/billing/history?before=not-a-cursor", token, "", 400)
	auditKey := verifyKeyAudit(t, pool.Pool, call, cfg.Identity, registration.Data.User.ID, wallet.Data.AccountID, token)
	verifyOIDCAssembly(t, h, cfg.OIDC, token)
	verifyCommunityFacade(t, pool.Pool, call, token)
	verifyMarketKeyAssembly(t, pool.Pool, call, token, registration.Data.User.ID)
	call("GET", "/api/marketplace/groups", "", "", 200)
	call("GET", "/api/marketplace/channels/mine", "", "", 401)
	call("GET", "/api/marketplace/channels/mine", token, "", 200)
	call("GET", "/api/marketplace/admin/channels", token, "", 403)
	call("POST", "/api/marketplace/channels", token, `{}`, 428)
	call("POST", "/api/user/policy-acceptance", token, `{"document":"supplier","version":"2026-10-07","locale":"en"}`, 200)
	call("POST", "/api/marketplace/channels", token, `{}`, 400)
	call("PUT", "/api/catalog/prices/model", token, `{}`, 403)
	call("GET", "/api/group-buy/list", token, "", 200)
	call("GET", "/api/blind-box/self", token, "", 200)
	call("GET", "/api/user/topup/info", token, "", 200)
	call("GET", "/api/community/v1/members/1", "", "", 503) // absent service secret explicitly disables the bridge
	call("GET", "/api/openapi.json", "", "", 200)
	call("GET", "/api/does-not-exist", "", "", 404)
	call("GET", "/.well-known/does-not-exist", "", "", 404)
	call("POST", "/api/status", "", "", 405)
	call("POST", "/api/log/self", token, "", 405)
	call("GET", "/dashboard", "", "", 200)
	if _, err := pool.Exec(ctx, `UPDATE v3_identity.users SET role='admin' WHERE id=$1`, registration.Data.User.ID); err != nil {
		t.Fatal(err)
	}
	call("GET", "/api/catalog/channels", token, "", 200)
	for _, path := range []string{"/api/catalog/models", "/api/models/", "/api/catalog/vendors", "/api/vendors/", "/api/catalog/prefill-groups", "/api/prefill_group/"} {
		call("GET", path, token, "", 200)
	}
	call("POST", "/api/catalog/models", token, `{`, 400)
	call("GET", "/api/catalog/channels", auditKey, "", 401)
	call("GET", "/api/log/", auditKey, "", 401)
	call("GET", "/api/wallet", auditKey, "", 401)
	call("GET", "/api/wallet/refunds/eligible", auditKey, "", 401)
	call("GET", "/api/marketplace/admin/channels", token, "", 200)
	call("GET", "/api/catalog/channels/not-an-integer", token, "", 400)
	call("PUT", "/api/catalog/prices/control-model", token, `{"input_per_mtok":-1}`, 400)
	call("PUT", "/api/catalog/prices/control-model", token, `{"mode":"per_token","input_per_mtok":1000000,"output_per_mtok":2000000}`, 200)
	call("GET", "/api/catalog/prices", token, "", 200)
	body := fmt.Sprintf(`{"account_id":%d,"amount_micro":1000000,"operation_id":"control-test","reason":"integration test"}`, wallet.Data.AccountID)
	call("POST", "/api/billing/adjustments", token, body, 200)
	call("POST", "/api/billing/adjustments", token, body, 200)
	call("POST", "/api/billing/adjustments", token, strings.Replace(body, "1000000", "2000000", 1), 409)
	w = call("GET", "/api/wallet", token, "", 200)
	if !strings.Contains(w.Body.String(), `"balance_micro_credits":"1000000"`) {
		t.Fatalf("duplicate adjustment changed balance: %s", w.Body)
	}
	verifyTypedRedemptionAssembly(t, pool.Pool, call, token)
	verifySecurityAuditAssembly(t, pool.Pool, call, token, registration.Data.User.ID, auditKey)
	verifyFundingEconomicsAssembly(t, pool.Pool, call, token, registration.Data.User.ID, auditKey)
	call("POST", "/api/user/logout", token, "", 200)
	call("GET", "/api/wallet", token, "", 401)
}

func clearComposedPaymentEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"V3_PAYMENT_PROVIDERS", "V3_STRIPE_SECRET_KEY", "V3_STRIPE_WEBHOOK_SECRET", "V3_PAYMENT_CURRENCY", "V3_TOPUP_CREDITS_PER_MINOR"} {
		t.Setenv(name, "")
	}
}
