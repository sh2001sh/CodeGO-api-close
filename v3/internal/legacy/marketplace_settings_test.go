package legacy

import (
	"encoding/json"
	"testing"
)

func TestMarketplaceRuntimeDefaultsForPersistedZeroSettings(t *testing.T) {
	d := marketplaceFixture(t)
	for _, key := range []string{"balance_blind_box_price_usd", "unit_price", "balance_blind_box_small_pity_guarantee_usd", "balance_blind_box_pity_guarantee_usd", "balance_blind_box_small_pity_threshold", "balance_blind_box_pity_threshold"} {
		row := marketplaceSourceRow{}
		row["key"], _ = json.Marshal("blind_box_setting." + key)
		row["value"] = json.RawMessage(`"0"`)
		d.source["options"] = append(d.source["options"], row)
	}
	marketplaceNormalize(d)
	var report Report
	d.validate(&report)
	if len(report.Issues) != 0 {
		t.Fatalf("runtime-normalized options rejected: %+v", report.Issues)
	}
	pool := marketplaceFind(t, d, "blind_box_pools", 1)
	if pool.Fields["price_micro"] != int64(2500000) || marketplaceFind(t, d, "blind_box_pools", 2).Fields["price_micro"] != int64(2500000) {
		t.Fatal("persisted zero setting became a free native pool")
	}
	var policy struct {
		Small      int64 `json:"small_after"`
		Big        int64 `json:"big_after"`
		SmallReset int64 `json:"small_reset_micro"`
		BigReset   int64 `json:"big_reset_micro"`
	}
	if err := json.Unmarshal(pool.Fields["guarantees"].(json.RawMessage), &policy); err != nil {
		t.Fatal(err)
	}
	if policy.Small != 10 || policy.Big != 50 || policy.SmallReset != 2500000 || policy.BigReset != 8750000 {
		t.Fatalf("effective guarantee defaults=%+v", policy)
	}
}

func TestMarketplaceProbabilityClampsMatchSourceRuntime(t *testing.T) {
	for _, tc := range []struct{ source, want string }{{"-0.01", "0"}, {"1.01", "1"}, {"0.003", "0.003"}, {"invalid", "invalid"}} {
		if got := marketplaceProbabilitySetting(tc.source); got != tc.want {
			t.Fatalf("%s => %s want %s", tc.source, got, tc.want)
		}
	}
}

func TestMarketplaceActiveMonthlyExpiryWithoutDurationOrOldBudgetSurvives(t *testing.T) {
	d := marketplaceFixture(t)
	d.source["blind_box_props"][0]["max_discount_quota"] = json.RawMessage(`0`)
	d.source["blind_box_props"][0]["used_discount_quota"] = json.RawMessage(`0`)
	marketplaceNormalize(d)
	var report Report
	d.validate(&report)
	if len(report.Issues) != 0 {
		t.Fatalf("valid existing active expiry was rejected: %+v", report.Issues)
	}
	card := marketplaceFind(t, d, "blind_box_props", 44)
	if card.Fields["duration_seconds"] != int64(0) || card.Fields["expires_at"] == nil || card.Fields["status"] != "active" {
		t.Fatalf("existing time state rewritten=%+v", card.Fields)
	}
}

func TestMarketplacePreviewRejectsNegativePityAndGhostReferences(t *testing.T) {
	d := marketplaceFixture(t)
	d.source["blind_box_pity_states"][0]["consecutive_low_rewards"] = json.RawMessage(`-1`)
	d.source["group_buy_members"][1]["order_id"] = json.RawMessage(`-1`)
	d.source["group_buy_members"][1]["user_subscription_id"] = json.RawMessage(`-1`)
	marketplaceNormalize(d)
	var report Report
	d.validate(&report)
	codes := map[string]bool{}
	for _, issue := range report.Issues {
		codes[issue.Code] = true
	}
	if !codes["invalid_blind_box_pity"] || !codes["invalid_ghost_membership"] {
		t.Fatalf("negative source states escaped preview: %+v", report.Issues)
	}
}
