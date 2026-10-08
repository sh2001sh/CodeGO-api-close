//go:build pgintegration

package catalogcontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/settings"
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
	rows, err := pool.Query(ctx, `SELECT nspname FROM pg_namespace WHERE nspname LIKE 'v3\_%' ESCAPE '\'`)
	if err != nil {
		t.Fatal(err)
	}
	var schemas []string
	for rows.Next() {
		var schema string
		if err = rows.Scan(&schema); err != nil {
			t.Fatal(err)
		}
		schemas = append(schemas, schema)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, schema := range schemas {
		if _, err = pool.Exec(ctx, `DROP SCHEMA `+pgx.Identifier{schema}.Sanitize()+` CASCADE`); err != nil {
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
		if _, err = pool.Exec(ctx, sql); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	return pool
}

func TestCatalogControlIntegration(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	enc, err := catalog.NewAESGCM(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	New(pool, enc, nil).Register(mux, func(h http.Handler) http.Handler { return h })
	call := func(method, path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		if w.Code != status {
			t.Fatalf("%s %s: %d, want %d: %s", method, path, w.Code, status, w.Body.String())
		}
		return w
	}
	call("PUT", "/api/catalog/groups/default", `{"multiplier":1}`, 200)
	call("PUT", "/api/catalog/groups/precise", `{"multiplier":1.123456}`, 200)
	w := call("POST", "/api/catalog/channels", `{"name":"alpha","provider":"openai","base_url":"https://example.test","models":["test-model"],"groups":["default"],"auto_disable":true}`, 200)
	var result struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.ID != 1 {
		t.Fatalf("channel id %d", result.Data.ID)
	}
	call("POST", "/api/catalog/channels/1/credentials", `{"secret":"never-return-secret","kind":"api_key","status":"enabled","max_concurrency":3,"fingerprint":{"user_agent":"stable-ua","tls_profile":"firefox"}}`, 200)
	call("PUT", "/api/catalog/channels/1/credentials/1", `{"max_concurrency":4}`, 200)
	for _, path := range []string{"/api/catalog/channels", "/api/catalog/channels/1", "/api/catalog/channels/1/credentials"} {
		w = call("GET", path, "", 200)
		if strings.Contains(w.Body.String(), "never-return-secret") {
			t.Fatalf("secret leaked from %s", path)
		}
	}
	var ciphertext []byte
	if err = pool.QueryRow(ctx, `SELECT secret FROM v3_catalog.channel_credentials WHERE channel_id=1`).Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ciphertext, []byte("never-return-secret")) {
		t.Fatal("plaintext credential persisted")
	}
	plain, err := enc.Decrypt(ciphertext)
	if err != nil || string(plain) != "never-return-secret" {
		t.Fatalf("ciphertext invalid: %v", err)
	}
	call("POST", "/api/catalog/channels/1/credentials", `{"secret":"oauth-secret","kind":"oauth","expires_at":"2030-01-01T00:00:00Z","max_concurrency":3}`, 200)
	call("PUT", "/api/catalog/channels/1/credentials/2", `{"status":"disabled"}`, 200)
	var kind string
	var expiry time.Time
	var concurrency int
	if err = pool.QueryRow(ctx, `SELECT kind,expires_at,max_concurrency FROM v3_catalog.channel_credentials WHERE id=2`).Scan(&kind, &expiry, &concurrency); err != nil || kind != "oauth" || expiry.Year() != 2030 || concurrency != 3 {
		t.Fatalf("status update changed credential identity: %s %s %d: %v", kind, expiry, concurrency, err)
	}
	// A failed membership change must roll back both channel and memberships.
	call("PUT", "/api/catalog/channels/1", `{"name":"broken","provider":"openai","groups":["missing"],"models":["other-model"]}`, 409)
	w = call("GET", "/api/catalog/channels/1", "", 200)
	if !strings.Contains(w.Body.String(), `"name":"alpha"`) || !strings.Contains(w.Body.String(), "test-model") {
		t.Fatal("failed channel update partially persisted")
	}
	call("PUT", "/api/catalog/prices/test-model", `{"mode":"per_token","input_per_mtok":1000000,"output_per_mtok":2000000}`, 200)
	call("PUT", "/api/catalog/prices/test-model", `{"mode":"expression","rules":{}}`, 400)
	call("PUT", "/api/catalog/route-pools", `{"group":"default","model":"test-model","strategy":"round_robin","enabled":true,"members":[{"channel_id":1,"priority":3,"weight":2}]}`, 200)
	w = call("GET", "/api/catalog/route-pools", "", 200)
	if !strings.Contains(w.Body.String(), `"channel_id":1`) {
		t.Fatal("pool member missing")
	}
	call("PUT", "/api/catalog/route-pools", `{"group":"default","model":"test-model","strategy":"round_robin","enabled":true,"members":[{"channel_id":1,"weight":1},{"channel_id":1,"weight":1}]}`, 400)
	var settingsBaseline int64
	if err = pool.QueryRow(ctx, `SELECT COALESCE(max(id),0) FROM v3_platform.cache_invalidation_outbox`).Scan(&settingsBaseline); err != nil {
		t.Fatal(err)
	}
	call("PUT", "/api/settings/SystemName", `{"value":"CodeGo"}`, 200)
	call("PUT", "/api/settings/OIDCClientSecret", `{"value":"hidden-secret"}`, 200)
	call("PUT", "/api/settings/arbitrary", `{"value":"first","sensitive":true}`, 200)
	call("PUT", "/api/settings/arbitrary", `{"value":"second","sensitive":false}`, 200)
	w = call("GET", "/api/settings", "", 200)
	if strings.Contains(w.Body.String(), "hidden-secret") || strings.Contains(w.Body.String(), "second") {
		t.Fatal("setting secret leaked")
	}
	value, err := settings.New(pool, enc).Get(ctx, "OIDCClientSecret")
	if err != nil || string(value) != `"hidden-secret"` {
		t.Fatalf("secret read %s: %v", value, err)
	}
	snap, err := catalog.Compile(ctx, pool, enc)
	if err != nil {
		t.Fatal(err)
	}
	if string(snap.Settings["SystemName"]) != `"CodeGo"` {
		t.Fatal("public setting missing from snapshot")
	}
	if _, ok := snap.Settings["OIDCClientSecret"]; ok {
		t.Fatal("secret entered snapshot")
	}
	if _, ok := snap.Settings["arbitrary"]; ok {
		t.Fatal("explicit sensitivity was downgraded")
	}
	if len(snap.Routes["default"]["test-model"]) != 1 || snap.Prices["test-model"].InputPerMTok != 1000000 {
		t.Fatal("admin catalog failed to compile")
	}
	if cr := snap.Channels[1].Credentials[0]; cr.MaxConcurrency != 4 || cr.Fingerprint.UserAgent != "stable-ua" || cr.Fingerprint.TLSProfile != "firefox" {
		t.Fatal("credential metadata did not survive partial update and PG Compile")
	}
	if snap.Groups["precise"].Multiplier != 1.123456 {
		t.Fatalf("group multiplier rounded early: %v", snap.Groups["precise"].Multiplier)
	}
	rows, err := pool.Query(ctx, `SELECT entity_id,count(*) FROM v3_platform.cache_invalidation_outbox WHERE entity='settings' AND id>$1 GROUP BY entity_id`, settingsBaseline)
	if err != nil {
		t.Fatal(err)
	}
	invalidations := make(map[string]int)
	for rows.Next() {
		var key string
		var count int
		if err = rows.Scan(&key, &count); err != nil {
			t.Fatal(err)
		}
		invalidations[key] = count
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(invalidations) != 3 || invalidations["SystemName"] != 1 || invalidations["OIDCClientSecret"] != 1 || invalidations["arbitrary"] != 2 {
		t.Fatalf("settings invalidations after baseline %d: %v", settingsBaseline, invalidations)
	}
	call("PUT", "/api/option/", `{"key":"Enabled","value":true}`, 200)
	w = call("GET", "/api/option/", "", 200)
	if !strings.Contains(w.Body.String(), `"value":"true"`) || strings.Contains(w.Body.String(), "OIDCClientSecret") {
		t.Fatal("legacy options response is incompatible")
	}
	call("DELETE", "/api/catalog/channels/1/credentials/999", "", 404)
	call("DELETE", "/api/settings/SystemName", "", 200)
	call("DELETE", "/api/settings/SystemName", "", 404)
	call("DELETE", "/api/catalog/route-pools/1", "", 200)
	call("DELETE", "/api/catalog/channels/1", "", 200)
	call("GET", "/api/catalog/channels/1", "", 404)
	// v2's existing channel payload uses numeric provider/status and comma lists.
	call("POST", "/api/channel/", `{"mode":"single","channel":{"name":"legacy","type":1,"key":"legacy-secret","status":1,"models":"test-model","group":"default"}}`, 200)
	call("PUT", "/api/channel/", `{"id":2,"name":"legacy-updated","type":1,"key":"additional-secret","key_mode":"append","status":2,"models":"test-model","group":"default"}`, 200)
	w = call("GET", "/api/channel/2", "", 200)
	if !strings.Contains(w.Body.String(), `"status":2`) || strings.Contains(w.Body.String(), "additional-secret") || strings.Contains(w.Body.String(), "legacy-secret") {
		t.Fatal("legacy channel compatibility failed")
	}
	var credentialCount int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_catalog.channel_credentials WHERE channel_id=2`).Scan(&credentialCount); err != nil || credentialCount != 2 {
		t.Fatalf("legacy append credential count %d: %v", credentialCount, err)
	}
	testProfiles(t, pool, enc)
}

func testProfiles(t *testing.T, pool *pgxpool.Pool, enc *catalog.AESGCM) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	var userID, wallet, subscription, planID int64
	if err := pool.QueryRow(ctx, `INSERT INTO v3_identity.users(username) VALUES('profile-test') RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',$1,'wallet') RETURNING id`, userID).Scan(&wallet); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO v3_commerce.plans(name,price_minor,credits,period_seconds) VALUES('profile-plan',100,1000,3600) RETURNING id`).Scan(&planID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('subscription',123,'subscription') RETURNING id`).Scan(&subscription); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO v3_commerce.subscriptions(user_id,plan_id,account_id,starts_at,expires_at) VALUES($1,$2,$3,$4,$5)`, userID, planID, subscription, now.Add(-time.Hour), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	cards, err := json.Marshal([]catalog.MultiplierCard{{MultiplierPPM: 500000, ExpiresAt: now.Add(time.Minute)}, {MultiplierPPM: 750000, ExpiresAt: now.Add(30 * time.Minute)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO v3_marketplace.account_profiles(user_id,multiplier_ppm,expires_at,cards) VALUES($1,500000,$2,$3)`, userID, now.Add(time.Minute), cards); err != nil {
		t.Fatal(err)
	}
	snap, err := catalog.Compile(ctx, pool, enc)
	if err != nil {
		t.Fatal(err)
	}
	p := snap.AccountProfiles[userID]
	if p.WalletAccountID != wallet || p.CardMultiplier(now) != 0.5 || len(p.SubscriptionAccounts(now)) != 1 || p.SubscriptionAccounts(now)[0] != subscription {
		t.Fatalf("compiled profile %+v", p)
	}
	if p.CardMultiplier(now.Add(time.Minute)) != 0.75 || p.CardMultiplier(now.Add(30*time.Minute)) != 1 || len(p.SubscriptionAccounts(now.Add(time.Hour))) != 0 {
		t.Fatal("expired profile still usable")
	}
}
