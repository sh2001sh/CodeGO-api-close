//go:build pgintegration

package marketplace

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func compatHTTP(t *testing.T, f *fixture, method, path, body, key string, admin bool, status int) *httptest.ResponseRecorder {
	t.Helper()
	handler := f.s.Handler(func(*http.Request) (int64, bool, error) { return 1, admin, nil })
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("%s %s: status=%d want=%d body=%s", method, path, w.Code, status, w.Body.String())
	}
	return w
}

func compatSeed(t *testing.T, f *fixture, scope string) Pool {
	t.Helper()
	p, err := f.s.SavePool(testContext, Pool{Name: scope, Enabled: true, Scope: scope, Price: 100, DailyLimit: 100,
		Rewards: []Reward{{Kind: "credits", Title: scope, Weight: 1, Amount: 10}}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestHTTPCompatDefaultCreditsPoolAndSimulation(t *testing.T) {
	f := newFixture(t)
	standard := compatSeed(t, f, "standard")
	creditsPool := compatSeed(t, f, "credits")
	compatHTTP(t, f, "POST", "/api/blind-box/inventory/purchase", `{"request_id":"old-purchase","count":1}`, "", false, 200)
	if f.balance(t, 1) != 9900 || f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_purchases WHERE pool_id=$1`, creditsPool.ID) != 1 {
		t.Fatal("legacy purchase failed to use credits pool/debit wallet")
	}
	w := compatHTTP(t, f, "POST", "/api/blind-box/simulation/draw", `{"count":1}`, "", false, 200)
	var response struct {
		Data struct {
			Draws []OpenRecord `json:"draws"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || len(response.Data.Draws) != 1 || response.Data.Draws[0].Reward.Title != "credits" {
		t.Fatalf("simulation pool %+v %v", response, err)
	}
	if _, err := f.pool.Exec(testContext, `UPDATE v3_marketplace.blind_box_pools SET enabled=false WHERE id=$1`, creditsPool.ID); err != nil {
		t.Fatal(err)
	}
	compatHTTP(t, f, "POST", "/api/blind-box/inventory/purchase", `{"request_id":"disabled","count":1}`, "", false, 503)
	compatHTTP(t, f, "POST", "/api/blind-box/simulation/draw", `{"count":1}`, "", false, 503)
	compatHTTP(t, f, "POST", "/api/blind-box/inventory/purchase", `{"request_id":"zero","pool_id":0,"count":1}`, "", false, 400)
	if f.balance(t, 1) != 9900 || f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_purchases WHERE pool_id=$1`, standard.ID) != 0 {
		t.Fatal("disabled credits pool silently used standard or charged wallet")
	}
	if f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_open_records`) != 0 {
		t.Fatal("simulation wrote history")
	}
}

func TestHTTPCompatExternalRecipientCannotBecomePrimaryKey(t *testing.T) {
	f := newFixture(t)
	p := compatSeed(t, f, "credits")
	if _, err := f.pool.Exec(testContext, `UPDATE v3_identity.users SET external_id=CASE id WHEN 2 THEN '3' WHEN 3 THEN 'OTHER3' END WHERE id IN(2,3)`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.PurchaseBoxes(testContext, 1, "stock", p.ID, 5); err != nil {
		t.Fatal(err)
	}
	path := "/api/blind-box/inventory/gift"
	compatHTTP(t, f, "POST", path, `{"request_id":"legacy","recipient_external_id":"3","count":1}`, "", false, 200)
	if f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_items WHERE owner_user_id=2`) != 1 || f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_items WHERE owner_user_id=3`) != 0 {
		t.Fatal("numeric external ID treated as primary key")
	}
	for _, body := range []string{
		`{"request_id":"mismatch","recipient_external_id":"OTHER3","recipient_id":2,"count":1}`,
		`{"request_id":"zero","recipient_external_id":"3","recipient_id":0,"count":1}`,
		`{"request_id":"blank","recipient_external_id":"","recipient_id":2,"count":1}`,
	} {
		compatHTTP(t, f, "POST", path, body, "", false, 400)
	}
	compatHTTP(t, f, "POST", path, `{"request_id":"agree","recipient_external_id":" other3 ","recipient_id":3,"count":1}`, "", false, 200)
	compatHTTP(t, f, "POST", path, `{"request_id":"native","recipient_id":3,"count":1}`, "", false, 200)
	if _, err := f.pool.Exec(testContext, `UPDATE v3_identity.users SET status='disabled' WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	compatHTTP(t, f, "POST", path, `{"request_id":"disabled","recipient_external_id":"3","count":1}`, "", false, 404)
	if _, err := f.pool.Exec(testContext, `UPDATE v3_identity.users SET deleted_at=now() WHERE id=3`); err != nil {
		t.Fatal(err)
	}
	compatHTTP(t, f, "POST", path, `{"request_id":"deleted","recipient_id":3,"count":1}`, "", false, 404)
	if f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_items WHERE owner_user_id=1`) != 2 || f.count(t, `SELECT count(*) FROM v3_marketplace.operations WHERE kind='gift'`) != 3 {
		t.Fatal("rejected recipient changed inventory or wrote receipt")
	}
}

func TestHTTPCompatPropGiftRecipientAndOwnership(t *testing.T) {
	f := newFixture(t)
	if _, err := f.pool.Exec(testContext, `UPDATE v3_identity.users SET external_id=CASE id WHEN 2 THEN '3' WHEN 3 THEN 'OTHER3' END WHERE id IN(2,3)`); err != nil {
		t.Fatal(err)
	}
	if err := monthlyGrant(f, 1, 60, "stock"); err != nil {
		t.Fatal(err)
	}
	path := "/api/blind-box/props/1/gift"
	compatHTTP(t, f, "POST", path, `{"request_id":"mismatch","recipient_external_id":"3","recipient_id":3}`, "", false, 400)
	w := compatHTTP(t, f, "POST", path, `{"request_id":"legacy","recipient_external_id":"3"}`, "", false, 200)
	if !strings.Contains(w.Body.String(), `"data":true`) || f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_props WHERE id=1 AND user_id=2`) != 1 {
		t.Fatal("legacy prop recipient failed or changed native boolean response")
	}
	compatHTTP(t, f, "POST", path, `{"request_id":"wrong-owner","recipient_id":3}`, "", false, 409)
	if f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_prop_gifts`) != 1 || f.count(t, `SELECT count(*) FROM v3_marketplace.operations WHERE kind='gift_prop'`) != 1 {
		t.Fatal("failed gift wrote audit or receipt")
	}
}

func TestHTTPCompatAdminAliasesAndReplay(t *testing.T) {
	f := newFixture(t)
	p := compatSeed(t, f, "credits")
	if _, err := f.pool.Exec(testContext, `UPDATE v3_identity.users SET role='admin' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	grant := "/api/blind-box/admin/users/2/grants"
	compatHTTP(t, f, "POST", grant, `{"quantity":5,"idempotency_key":"legacy-grant","reason":"retained reason"}`, "", true, 200)
	compatHTTP(t, f, "POST", grant, `{"quantity":5,"idempotency_key":"legacy-grant","reason":"retained reason"}`, "", true, 200)
	if f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_grants WHERE quantity=5 AND reason='retained reason'`) != 1 || f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_items WHERE owner_user_id=2`) != 5 {
		t.Fatal("legacy grant failed to preserve reason/replay")
	}
	for _, body := range []string{
		`{"quantity":1,"count":2,"request_id":"count-conflict"}`,
		`{"quantity":1,"request_id":"one","idempotency_key":"two"}`,
		`{"quantity":1,"count":0,"idempotency_key":"zero"}`,
		`{"quantity":1,"idempotency_key":""}`,
	} {
		compatHTTP(t, f, "POST", grant, body, "", true, 400)
	}
	compatHTTP(t, f, "POST", grant, `{"quantity":4,"idempotency_key":"legacy-grant","reason":"retained reason"}`, "", true, 409)
	compatHTTP(t, f, "POST", grant, fmt.Sprintf(`{"pool_id":%d,"count":1,"request_id":"native-grant"}`, p.ID), "", true, 200)
	longKey := strings.Repeat("g", 128)
	compatHTTP(t, f, "POST", grant, fmt.Sprintf(`{"quantity":1,"idempotency_key":%q}`, longKey), "", true, 200)
	compatHTTP(t, f, "POST", grant, fmt.Sprintf(`{"quantity":1,"idempotency_key":%q}`, longKey), "", true, 200)
	if f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_items WHERE owner_user_id=2 AND status='available'`) != 7 {
		t.Fatal("native grant or 128-char legacy key did not replay")
	}
	first := compatHTTP(t, f, "DELETE", grant, `{"quantity":1,"reason":"one"}`, "", true, 200)
	second := compatHTTP(t, f, "DELETE", grant, `{"quantity":1,"reason":"one"}`, "", true, 200)
	if first.Header().Get("Idempotency-Key") == "" || first.Header().Get("Idempotency-Key") == second.Header().Get("Idempotency-Key") || !strings.Contains(first.Body.String(), `"revoked":1`) {
		t.Fatal("unkeyed repeated revokes collapsed into one operation")
	}
	compatHTTP(t, f, "DELETE", grant, `{"quantity":1,"reason":"header audit"}`, "header-revoke", true, 200)
	compatHTTP(t, f, "DELETE", grant, `{"quantity":1,"reason":"header audit"}`, "header-revoke", true, 200)
	compatHTTP(t, f, "DELETE", grant, `{"quantity":1,"reason":"different reason"}`, "header-revoke", true, 409)
	if f.count(t, `SELECT count(*) FROM v3_marketplace.operations WHERE kind='revoke' AND response->>'reason'='one'`) != 2 || f.count(t, `SELECT count(*) FROM v3_marketplace.operations WHERE kind='revoke' AND request_id='header-revoke' AND response->>'reason'='header audit' AND jsonb_array_length(response->'item_ids')=1`) != 1 {
		t.Fatal("revoke audit reason/item IDs missing or replay changed original receipt")
	}
	compatHTTP(t, f, "DELETE", grant, fmt.Sprintf(`{"quantity":1,"reason":%q}`, strings.Repeat("r", 2001)), "", true, 400)
	w := compatHTTP(t, f, "POST", "/api/blind-box/admin/users/2/revoke", `{"count":1,"request_id":"native-revoke"}`, "", true, 200)
	if !strings.Contains(w.Body.String(), `"data":[`) {
		t.Fatal("native revoke array response changed")
	}
	compatHTTP(t, f, "DELETE", grant, `{"quantity":1}`, "", false, 403)
	failed := compatHTTP(t, f, "DELETE", grant, `{"quantity":100}`, "failed-revoke", true, 409)
	if strings.Contains(failed.Body.String(), `"revoked"`) {
		t.Fatal("failed atomic revoke claimed a partial revoke in its response")
	}
	if f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_items WHERE owner_user_id=2 AND status='available'`) != 3 || f.count(t, `SELECT count(*) FROM v3_marketplace.operations WHERE kind='revoke'`) != 4 {
		t.Fatal("failed revoke mutated inventory or created receipt")
	}
}
