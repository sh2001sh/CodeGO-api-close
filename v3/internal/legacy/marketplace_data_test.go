package legacy

import (
	"encoding/json"
	"math"
	"testing"
)

func marketplaceFixtureRow(t *testing.T, raw string) marketplaceSourceRow {
	t.Helper()
	var row marketplaceSourceRow
	if err := json.Unmarshal([]byte(raw), &row); err != nil {
		t.Fatal(err)
	}
	return row
}
func marketplaceFixture(t *testing.T) *marketplaceData {
	t.Helper()
	d := &marketplaceData{source: make(map[string][]marketplaceSourceRow)}
	fixture := map[string][]string{
		"users":               {`{"id":7}`, `{"id":8}`},
		"subscription_plans":  {`{"id":3,"title":"Lite月卡","group_buy_bonus2":1.25,"group_buy_bonus3":2.5,"group_buy_bonus5":5}`},
		"subscription_orders": {`{"id":4,"user_id":7}`}, "user_subscriptions": {`{"id":9,"user_id":7,"plan_id":3}`},
		"group_buy_orders":              {`{"id":10,"initiator_id":7,"plan_id":3,"target_count":5,"current_count":2,"status":"pending","expires_at":1800000500,"settled_at":0,"created_at":1800000000,"updated_at":1800000001}`},
		"group_buy_members":             {`{"id":100,"group_buy_id":10,"user_id":7,"order_id":4,"user_subscription_id":9,"bonus_granted":true,"bonus_amount_usd":1.25,"created_at":1800000000}`, `{"id":101,"group_buy_id":10,"user_id":8,"order_id":0,"user_subscription_id":0,"bonus_granted":false,"bonus_amount_usd":0,"created_at":1800000000}`},
		"balance_blind_box_purchases":   {`{"id":51,"user_id":7,"request_id":"purchase-51","quantity":2,"unit_price_usd":2.5,"total_quota":2500000,"purchase_date":"2027-01-15","status":"completed","created_at":1800000000}`},
		"balance_blind_box_items":       {`{"id":52,"purchase_id":51,"purchase_user_id":7,"owner_user_id":7,"pool_version":"legacy-frozen","reward_type":"claude_quota","reward_usd":5,"credit_amount":2500000,"reward_title":"reward","reward_tier":"five","reward_wallet_type":"claude","guarantee_type":"first","status":"opened","open_record_id":61,"created_at":1800000000,"updated_at":1800000001,"opened_at":1800000001}`, `{"id":53,"purchase_id":51,"purchase_user_id":7,"owner_user_id":8,"pool_version":"unified-box-v4-open-draw","reward_type":"","guarantee_type":"none","status":"available","open_record_id":0,"created_at":1800000000,"updated_at":1800000001,"opened_at":0}`},
		"blind_box_open_records":        {`{"id":61,"user_id":7,"order_id":0,"reward_type":"claude_quota","reward_wallet_type":"claude","reward_usd":5,"credit_amount":2500000,"reward_title":"reward","reward_tier":"five","pool_type":"unified","is_pity":true,"create_time":1800000001,"request_id":"open-61"}`, `{"id":62,"user_id":7,"order_id":0,"reward_type":"prop","reward_title":"card","pool_type":"standard","is_pity":false,"create_time":1800000002}`},
		"blind_box_props":               {`{"id":44,"user_id":7,"open_record_id":62,"prop_type":"monthly_pass_multiplier","title":"card","status":"active","discount_rate":0.9,"multiplier":0.1,"duration_seconds":0,"remaining_seconds":0,"max_discount_quota":1000000,"used_discount_quota":500000,"activated_at":1800000002,"expires_at":1800000500,"created_at":1800000002,"updated_at":1800000002}`},
		"balance_blind_box_pity_states": {`{"id":70,"user_id":7,"consecutive_under6_usd":2,"consecutive_under35_usd":3,"updated_at":1800000002}`},
		"blind_box_pity_states":         {`{"id":71,"user_id":7,"consecutive_low_rewards":4,"updated_at":1800000002}`},
	}
	for name, rows := range fixture {
		for _, raw := range rows {
			d.source[name] = append(d.source[name], marketplaceFixtureRow(t, raw))
		}
	}
	return d
}

func TestMarketplaceStandardPolicyUsesEffectiveRuntimeConfiguration(t *testing.T) {
	d := marketplaceFullFixture(t)
	options := map[string]string{
		"enabled": "true", "daily_limit": "500", "monthly_limit": "123", "daily_open_limit": "321",
		"subscription_prize_probability": "0.125001", "subscription_plan_title": "Lite月卡",
		"first_purchase_guarantee_usd": "100", "pity_guarantee_usd": "200", "pity_threshold": "2", "low_reward_threshold_usd": "5",
		"tiers":                   `[{"name":"dormant tier","min_usd":99,"max_usd":99,"probability":1,"reward_type":"quota"}]`,
		"balance_blind_box_tiers": `[{"name":"effective tier","min_usd":2,"max_usd":2,"probability":1,"reward_type":"quota","wallet_type":"default"}]`,
	}
	for key, value := range options {
		row := marketplaceSourceRow{}
		row["key"], _ = json.Marshal("blind_box_setting." + key)
		row["value"], _ = json.Marshal(value)
		d.source["options"] = append(d.source["options"], row)
	}
	marketplaceNormalize(d)
	var report Report
	d.validate(&report)
	if len(report.Issues) != 0 {
		t.Fatalf("conditional policy blocked: %+v", report.Issues)
	}
	pool := marketplaceFind(t, d, "blind_box_pools", 2)
	if pool.Fields["daily_limit"] != int64(10) || pool.Fields["monthly_limit"] != int64(123) || pool.Fields["daily_open_limit"] != int64(321) {
		t.Fatalf("effective purchase/open limits=%+v", pool.Fields)
	}
	var policy struct {
		Enabled     bool  `json:"enabled"`
		Probability int64 `json:"subscription_probability_ppb"`
		Plan        int64 `json:"subscription_plan_id"`
		First       int64 `json:"first_purchase_minimum_micro"`
		Pity        int64 `json:"pity_minimum_micro"`
		Low         int64 `json:"low_reward_threshold_micro"`
		After       int64 `json:"pity_after"`
	}
	if err := json.Unmarshal(pool.Fields["standard_policy"].(json.RawMessage), &policy); err != nil {
		t.Fatal(err)
	}
	if !policy.Enabled || policy.Probability != 125001000 || policy.Plan != 3 || policy.First != 0 || policy.Pity != 0 || policy.Low != 0 || policy.After != 1000000 {
		t.Fatalf("effective conditional policy=%+v", policy)
	}
	var rewards []struct {
		Minimum int64  `json:"minimum_micro"`
		Type    string `json:"legacy_reward_type"`
		Wallet  string `json:"wallet_type"`
	}
	if err := json.Unmarshal(pool.Fields["rewards"].(json.RawMessage), &rewards); err != nil {
		t.Fatal(err)
	}
	if len(rewards) != 1 || rewards[0].Minimum != 2000000 || rewards[0].Type != "claude_quota" || rewards[0].Wallet != "claude" {
		t.Fatalf("effective reward tiers=%+v", rewards)
	}
}

func TestMarketplaceMissingSubscriptionPrizePlanBlocksActivePool(t *testing.T) {
	d := marketplaceFullFixture(t)
	d.source["options"] = []marketplaceSourceRow{
		marketplaceFixtureRow(t, `{"key":"blind_box_setting.enabled","value":"true"}`),
		marketplaceFixtureRow(t, `{"key":"blind_box_setting.subscription_plan_title","value":"missing monthly plan"}`),
	}
	marketplaceNormalize(d)
	var report Report
	d.validate(&report)
	if len(report.Issues) != 1 || report.Issues[0].Code != "missing_standard_subscription_plan" {
		t.Fatalf("missing plan would silently lose conditional reward: %+v", report.Issues)
	}
}

func TestMarketplacePoolConfigurationWithoutPurchasesIsRetained(t *testing.T) {
	d := &marketplaceData{source: map[string][]marketplaceSourceRow{
		"options": {marketplaceFixtureRow(t, `{"key":"blind_box_setting.balance_blind_box_daily_purchase_limit","value":"500"}`)},
	}}
	marketplaceNormalize(d)
	var report Report
	d.validate(&report)
	if len(report.Issues) != 0 || report.Counts["marketplace.blind_box_pools"] != 2 || marketplaceFind(t, d, "blind_box_pools", 1).Fields["daily_limit"] != int64(500) {
		t.Fatalf("unused but configured pools disappeared: %+v", report)
	}
}

func marketplaceFullFixture(t *testing.T) *marketplaceData {
	d := marketplaceFixture(t)
	extra := map[string][]string{
		"blind_box_orders":               {`{"id":1,"user_id":7,"quantity":3,"opened_count":1,"money":7.50,"trade_no":"legacy-standard-stock","payment_provider":"epay","payment_method":"alipay","source":"purchase","status":"success","create_time":1800000000,"complete_time":1800000001,"expires_at":1800000500,"provider_payload":"paid"}`, `{"id":2,"user_id":7,"quantity":2,"opened_count":0,"money":5,"trade_no":"already-issued-cash","payment_provider":"xunhu","source":"purchase","status":"success","create_time":1800000000,"complete_time":1800000001}`},
		"blind_box_grants":               {`{"id":11,"user_id":7,"admin_user_id":8,"blind_box_order_id":1,"quantity":3,"reason":"fixture","idempotency_key":"grant-11","trade_no":"grant-11","created_at":1800000000}`},
		"blind_box_credits":              {`{"id":12,"user_id":7,"open_record_id":61,"original_amount":100,"remaining_amount":0,"reward_usd":0.0002,"status":"exhausted","created_at":1800000000,"updated_at":1800000001}`},
		"blind_box_prop_gifts":           {`{"id":13,"request_id":"prop-gift-13","prop_id":44,"sender_user_id":8,"recipient_user_id":7,"sender_external_id":"sender","recipient_external_id":"recipient","prop_type":"monthly_pass_multiplier","prop_title":"card","status":"completed","created_at":1800000001}`},
		"blind_box_prop_discount_usages": {`{"id":14,"request_id":"usage-14","user_id":7,"prop_id":44,"prop_title":"card","channel_id":99,"channel_scope":"official","model_name":"model","quota_before_discount":100,"quota_after_discount":80,"discount_quota":20,"discount_rate":0.2,"multiplier":0.8,"effective_multiplier":0.8,"remaining_quota":500000,"created_at":1800000001}`},
		"balance_blind_box_gifts":        {`{"id":15,"request_id":"gift-15","sender_user_id":7,"recipient_user_id":8,"sender_external_id":"sender","recipient_external_id":"recipient","sender_display_name_masked":"a***","recipient_display_name_masked":"b***","quantity":1,"status":"completed","created_at":1800000001}`},
		"balance_blind_box_gift_items":   {`{"id":16,"gift_id":15,"item_id":53,"from_user_id":7,"to_user_id":8,"created_at":1800000001}`},
		"blind_box_zero_hour_states":     {`{"id":17,"user_id":7,"points":100,"usage_quota":500,"hit_count":2,"updated_at":1800000001}`},
	}
	for name, rows := range extra {
		for _, raw := range rows {
			d.source[name] = append(d.source[name], marketplaceFixtureRow(t, raw))
		}
	}
	d.source["blind_box_props"] = append(d.source["blind_box_props"], marketplaceFixtureRow(t, `{"id":45,"user_id":8,"open_record_id":0,"prop_type":"monthly_pass_multiplier","title":"15 分钟 0.1 倍率卡","status":"available","discount_rate":0.9,"multiplier":0.1,"duration_seconds":900,"remaining_seconds":900,"max_discount_quota":0,"used_discount_quota":0,"benefit_reference":"subscription-order:2","created_at":1800000001,"updated_at":1800000001}`))
	d.source["balance_blind_box_purchases"][0]["request_id"], _ = json.Marshal(marketplaceCashRequest("already-issued-cash"))
	return d
}
func marketplaceNormalize(d *marketplaceData) {
	d.normalizeGroups()
	d.normalizeBoxOrders()
	d.normalizeBlindBoxes()
	d.normalizeBoxOrderInventory()
	d.normalizeProps()
	d.normalizeBoxHistory()
}
func marketplaceFind(t *testing.T, d *marketplaceData, table string, id int64) marketplaceRecord {
	t.Helper()
	for _, r := range d.records {
		if r.Table == table && r.ID == id {
			return r
		}
	}
	t.Fatalf("missing %s %d", table, id)
	return marketplaceRecord{}
}

func TestMarketplaceProjectionPreservesMoneyGhostsRewardsAndSeparatePity(t *testing.T) {
	d := marketplaceFixture(t)
	marketplaceNormalize(d)
	var report Report
	d.validate(&report)
	if len(report.Issues) != 0 {
		t.Fatalf("issues=%+v", report.Issues)
	}
	member := marketplaceFind(t, d, "group_buy_members", 100)
	if member.Fields["order_id"] != int64(9) || member.Fields["bonus_amount_micro"] != int64(1250000) {
		t.Fatalf("member=%+v", member)
	}
	ghost := marketplaceFind(t, d, "group_buy_members", 101)
	if ghost.Fields["order_id"] != int64(0) || ghost.Fields["subscription_id"] != nil {
		t.Fatalf("ghost=%+v", ghost)
	}
	item := marketplaceFind(t, d, "blind_box_items", 52)
	var rewards []map[string]json.RawMessage
	if err := json.Unmarshal(item.Fields["rewards"].([]byte), &rewards); err != nil {
		t.Fatal(err)
	}
	if len(rewards) != 1 || string(rewards[0]["amount_micro"]) != "5000000" {
		t.Fatalf("frozen=%s", item.Fields["rewards"])
	}
	dynamic := marketplaceFind(t, d, "blind_box_items", 53)
	if string(dynamic.Fields["guarantees"].(json.RawMessage)) == "{}" {
		t.Fatal("undecided item lost pool guarantee")
	}
	prop := marketplaceFind(t, d, "blind_box_props", 44)
	if prop.Fields["max_discount_micro"] != int64(2000000) || prop.Fields["used_discount_micro"] != int64(1000000) || prop.Fields["multiplier_ppm"] != int64(100000) {
		t.Fatalf("capped prop=%+v", prop)
	}
	if report.Counts["marketplace.blind_box_pity"] != 2 {
		t.Fatalf("counts=%+v", report.Counts)
	}
}

func TestMarketplaceExactDecimalAndProbabilityBoundaries(t *testing.T) {
	for _, tc := range []struct {
		raw         string
		scale, want int64
		fail        bool
	}{{"9007199254.740991", 1000000, 9007199254740991, false}, {"9223372036854.775807", 1000000, math.MaxInt64, false}, {"9223372036854.775808", 1000000, 0, true}, {"0.0000001", 1000000, 0, true}, {"-1", 1000000, 0, true}} {
		got, err := marketplaceExact(tc.raw, tc.scale)
		if (err != nil) != tc.fail || (!tc.fail && got != tc.want) {
			t.Errorf("%s got=%d err=%v", tc.raw, got, err)
		}
	}
	tiers := []marketplaceTier{{Probability: json.Number("0.00000001")}, {Probability: json.Number("0.52177312")}}
	weights, err := marketplaceWeights(tiers)
	if err != nil || weights[0] != 1 || weights[1] != 52177312 {
		t.Fatalf("weights=%v err=%v", weights, err)
	}
	for _, value := range []string{"-0.1", "bogus", "0"} {
		_, err = marketplaceWeights([]marketplaceTier{{Probability: json.Number(value)}})
		if err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
}

func TestMarketplaceRejectsBrokenReferencesButExcludesRetiredPoints(t *testing.T) {
	d := marketplaceFixture(t)
	d.source["group_buy_members"][0]["user_subscription_id"] = json.RawMessage(`999`)
	d.source["blind_box_credits"] = []marketplaceSourceRow{marketplaceFixtureRow(t, `{"id":88,"user_id":7,"open_record_id":61,"original_amount":100,"remaining_amount":50,"reward_usd":0.0002,"status":"active","migrated_at":0,"created_at":1800000000,"updated_at":1800000000}`)}
	d.source["balance_blind_box_items"][0]["credit_amount"] = json.RawMessage(`9223372036854775807`)
	marketplaceNormalize(d)
	var report Report
	d.validate(&report)
	codes := map[string]bool{}
	for _, issue := range report.Issues {
		codes[issue.Code] = true
	}
	for _, code := range []string{"invalid_group_subscription", "unsupported_blind_box_reward"} {
		if !codes[code] {
			t.Errorf("missing %s: %+v", code, report.Issues)
		}
	}
	if codes["unmigrated_blind_box_credit"] || report.Counts["marketplace.blind_box_credits"] != 0 || report.Counts["retired_features.blind_box_credits"] != 1 || report.Amounts["retired_features.blind_box_credits.remaining_amount_v2_units"] != "50" {
		t.Fatalf("retired points blocked import or became money: %+v", report)
	}
}

func TestMarketplaceExternalInventoryIssuanceIsNotDuplicated(t *testing.T) {
	d := marketplaceFullFixture(t)
	marketplaceNormalize(d)
	var report Report
	d.validate(&report)
	if len(report.Issues) > 0 {
		t.Fatalf("issues=%+v", report.Issues)
	}
	if got := report.Counts["marketplace.blind_box_purchases"]; got != 2 {
		t.Fatalf("purchases=%d want existing cash and one synthetic legacy purchase", got)
	}
	if got := report.Counts["marketplace.blind_box_items"]; got != 4 {
		t.Fatalf("items=%d want original 2 + remaining legacy 2", got)
	}
	order := marketplaceFind(t, d, "blind_box_orders", 1)
	if order.Fields["amount_minor"] != int64(750) || order.Fields["currency"] != "cny" {
		t.Fatalf("cash order=%+v", order)
	}
	if got := marketplaceFind(t, d, "blind_box_purchases", 51).Fields["external_order_id"]; got != int64(2) {
		t.Fatalf("cash association=%v", got)
	}
	if got := report.Amounts["marketplace.blind_box_prop_discount_usages.discount_micro"]; got != "40" {
		t.Fatalf("discount amount=%s", got)
	}
}
