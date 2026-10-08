//go:build pgintegration

package marketplace

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestAdminPoolsIncludesDisabledAndRoundTripsFullPolicy(t *testing.T) {
	f := newFixture(t)
	enabled := f.seedPool(t, Reward{Kind: "credits", Amount: 10})
	disabled := Pool{
		Name: "disabled standard", Price: 100, DailyLimit: 10, MonthlyLimit: 20, DailyOpenLimit: 7, Scope: "standard",
		Standard:   StandardPolicy{Enabled: true, SubscriptionProbabilityPPB: 100, SubscriptionPlanID: 3, FirstPurchaseMinimumMicro: 200, PityMinimumMicro: 100, LowRewardThresholdMicro: 5, PityAfter: 5},
		Rewards:    []Reward{{Kind: "credits", Title: "range", Weight: 9223372036854775807, Minimum: 50, Maximum: 100, Step: 5, WalletType: "bonus", RewardTier: "rare", LegacyRewardType: "credits"}},
		Guarantees: Guarantees{First: []Reward{{Kind: "topup_discount", Title: "coupon", Weight: 1, DiscountRatePPM: 900000, MaxDiscountMicro: 1000, PropType: "topup_discount_90"}}},
	}
	var err error
	disabled, err = f.s.SavePool(testContext, disabled)
	if err != nil {
		t.Fatal(err)
	}
	h := f.s.Handler(func(*http.Request) (int64, bool, error) { return 1, true, nil })
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/blind-box/admin/pools", nil))
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d cache=%q", w.Code, w.Header().Get("Cache-Control"))
	}
	var envelope struct {
		Success bool   `json:"success"`
		Data    []Pool `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.Success || len(envelope.Data) != 2 || !reflect.DeepEqual(envelope.Data, []Pool{enabled, disabled}) {
		t.Fatalf("unexpected admin pools: %+v", envelope)
	}
	if _, err := f.s.SavePool(testContext, envelope.Data[1]); err != nil {
		t.Fatal(err)
	}
	pools, err := f.s.AdminPools(testContext)
	if err != nil || !reflect.DeepEqual(pools[1], disabled) {
		t.Fatalf("roundtrip lost fields: %+v err=%v", pools, err)
	}
	disabled.Enabled = true
	if _, err := f.s.SavePool(testContext, disabled); err != nil {
		t.Fatal(err)
	}
	public, err := f.s.loadEnabledPools(testContext)
	if err != nil || len(public) != 2 {
		t.Fatalf("re-enabled pool missing: %+v err=%v", public, err)
	}
}

func TestAdminPoolsEmptyList(t *testing.T) {
	f := newFixture(t)
	pools, err := f.s.AdminPools(testContext)
	if err != nil || pools == nil || len(pools) != 0 {
		t.Fatalf("pools=%+v err=%v", pools, err)
	}
}

func TestAdminPoolsInvalidStoredPolicyFailsExplicitly(t *testing.T) {
	f := newFixture(t)
	p := f.seedPool(t, Reward{Kind: "credits", Amount: 10})
	if _, err := f.pool.Exec(testContext, `UPDATE v3_marketplace.blind_box_pools SET rewards='[{"kind":"unknown","title":"invalid","weight":1}]'::jsonb WHERE id=$1`, p.ID); err != nil {
		t.Fatal(err)
	}
	h := f.s.Handler(func(*http.Request) (int64, bool, error) { return 1, true, nil })
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/blind-box/admin/pools", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("invalid stored pool status=%d body=%s", w.Code, w.Body.String())
	}
}
