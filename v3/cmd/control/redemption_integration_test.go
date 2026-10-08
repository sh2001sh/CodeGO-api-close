//go:build pgintegration

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/internal/identity"
)

// Exercise the actual control callbacks and session boundary for all three benefits.
func verifyTypedRedemptionAssembly(t *testing.T, pool *pgxpool.Pool, call apiCall, admin string) {
	t.Helper()
	ctx := context.Background()
	w := call("POST", "/api/user/register", "", `{"username":"redemption_user","password":"strong-password","accepted_terms_version":"2026-10-07","accepted_privacy_version":"2026-10-07","agreement_locale":"en"}`, 200)
	var session struct {
		Data identity.Session `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil || session.Data.AccessToken == "" {
		t.Fatalf("redemption user registration: %v", err)
	}
	user := session.Data.AccessToken
	call("GET", "/api/redemption/", user, "", 403)
	call("POST", "/api/redemption/", user, `{"name":"denied","credits":1}`, 403)
	call("POST", "/api/commerce/redemptions/redeem", "", `{"key":"missing"}`, 401)
	for _, body := range []string{
		`{"name":"mixed","credits":1,"redeem_type":"subscription","plan_id":1}`,
		`{"name":"missing plan","credits":0,"redeem_type":"subscription"}`,
		`{"name":"too many","credits":0,"redeem_type":"blind_box","blind_box_quantity":101}`,
		`{"name":"obsolete","credits":1,"redeem_type":"quota"}`,
	} {
		call("POST", "/api/redemption/", admin, body, 400)
	}
	issue := func(body string) commerce.RedemptionCode {
		t.Helper()
		response := call("POST", "/api/redemption/", admin, body, 200)
		var result struct {
			Data commerce.RedemptionCode `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Data.Key == "" {
			t.Fatalf("typed redemption issuance: %v", err)
		}
		return result.Data
	}
	redeem := func(path string, code commerce.RedemptionCode) commerce.RedemptionResult {
		t.Helper()
		body, err := json.Marshal(map[string]string{"key": code.Key})
		if err != nil {
			t.Fatal(err)
		}
		response := call("POST", path, user, string(body), 200)
		var result struct {
			Data commerce.RedemptionResult `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatalf("typed redemption result: %v", err)
		}
		return result.Data
	}
	checkUnclaimed := func(code commerce.RedemptionCode) {
		t.Helper()
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.redemption_codes WHERE id=$1 AND state='active' AND claimed_by IS NULL AND redeem_result IS NULL`, code.ID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("failed benefit consumed redemption: count=%d err=%v", count, err)
		}
	}
	money := issue(`{"name":"wallet","credits":5000}`)
	if money.RedeemType != "credits" {
		t.Fatalf("default native redemption type=%q", money.RedeemType)
	}
	result := redeem("/api/commerce/redemptions/redeem", money)
	if result.RedeemType != "credits" || result.Credits != 5000 || result.PlanID != 0 || result.BlindBoxOrderID != 0 {
		t.Fatalf("wallet result=%+v", result)
	}
	if replay := redeem("/api/user/topup", money); replay != result {
		t.Fatalf("wallet alias replay changed result=%+v", replay)
	}
	w = call("POST", "/api/subscription/admin/plans", admin, `{"name":"Redeemed package","price_minor":1000,"currency":"usd","credits":5000,"period_credits":1000,"period_seconds":86400,"reset_period":"daily","enabled":true,"max_purchase_per_user":1}`, 200)
	var plan struct {
		Data commerce.Plan `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &plan); err != nil || plan.Data.ID <= 0 {
		t.Fatalf("redemption plan: %v", err)
	}
	subscriptionBody := fmt.Sprintf(`{"name":"package","credits":0,"redeem_type":"subscription","plan_id":%d}`, plan.Data.ID)
	packageCode := issue(subscriptionBody)
	result = redeem("/api/user/topup", packageCode)
	if result.RedeemType != "subscription" || result.PlanID != plan.Data.ID || result.PlanTitle != plan.Data.Name || result.UserSubscriptionID <= 0 || result.Credits != 0 {
		t.Fatalf("subscription result=%+v", result)
	}
	if replay := redeem("/api/commerce/redemptions/redeem", packageCode); replay != result {
		t.Fatalf("subscription replay changed result=%+v", replay)
	}
	capped := issue(subscriptionBody)
	body, _ := json.Marshal(map[string]string{"key": capped.Key})
	call("POST", "/api/commerce/redemptions/redeem", user, string(body), 409)
	checkUnclaimed(capped)
	call("DELETE", fmt.Sprintf("/api/redemption/%d", capped.ID), admin, "", 200)
	call("POST", "/api/commerce/redemptions/redeem", user, string(body), 409)
	boxes := issue(`{"name":"sealed boxes","credits":0,"redeem_type":"blind_box","blind_box_quantity":3}`)
	body, _ = json.Marshal(map[string]string{"key": boxes.Key})
	call("POST", "/api/commerce/redemptions/redeem", user, string(body), 503)
	checkUnclaimed(boxes)
	call("PUT", "/api/blind-box/admin/pools", admin, `{"name":"Redemption standard pool","scope":"standard","enabled":true,"price_micro":2500000,"daily_limit":100,"rewards":[{"kind":"credits","title":"reward","weight":1,"amount_micro":1000}]}`, 200)
	result = redeem("/api/commerce/redemptions/redeem", boxes)
	if result.RedeemType != "blind_box" || result.BlindBoxQuantity != 3 || result.BlindBoxOrderID <= 0 || result.Credits != 0 {
		t.Fatalf("blind box result=%+v", result)
	}
	if replay := redeem("/api/user/topup", boxes); replay != result {
		t.Fatalf("blind box replay changed result=%+v", replay)
	}
	call("POST", "/api/commerce/redemptions/redeem", admin, string(body), 409)
	var items, packages int
	var balance int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_marketplace.blind_box_items WHERE owner_user_id=$1 AND status='available'`, session.Data.User.ID).Scan(&items); err != nil || items != 3 {
		t.Fatalf("sealed redemption inventory=%d err=%v", items, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.subscriptions WHERE user_id=$1 AND source='redemption'`, session.Data.User.ID).Scan(&packages); err != nil || packages != 1 {
		t.Fatalf("redemption packages=%d err=%v", packages, err)
	}
	if err := pool.QueryRow(ctx, `SELECT balance FROM v3_billing.accounts WHERE owner_id=$1 AND owner_type='user' AND kind='wallet'`, session.Data.User.ID).Scan(&balance); err != nil || balance != 5000 {
		t.Fatalf("nonmonetary benefits changed wallet=%d err=%v", balance, err)
	}
	var listed struct {
		Data []commerce.RedemptionCode `json:"data"`
	}
	w = call("GET", "/api/redemption/", admin, "", 200)
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil || len(listed.Data) != 4 {
		t.Fatalf("redemption admin list count=%d err=%v", len(listed.Data), err)
	}
	for _, code := range listed.Data {
		if code.Key != "" || code.RedeemType == "" {
			t.Fatal("list disclosed raw key or lost native benefit type")
		}
	}
}
