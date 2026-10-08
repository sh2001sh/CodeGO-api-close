package legacy

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
)

func entitlementFixture(t *testing.T) *entitlementsData {
	t.Helper()
	d := &entitlementsData{rows: map[string][]commerceRow{}, refs: map[string]map[int64]commerceRow{}}
	add := func(table string, values map[string]any) {
		encoded, err := json.Marshal(values)
		if err != nil {
			t.Fatal(err)
		}
		r := commerceRow{}
		if err = json.Unmarshal(encoded, &r); err != nil {
			t.Fatal(err)
		}
		d.rows[table] = append(d.rows[table], r)
		if d.refs[table] == nil {
			d.refs[table] = map[int64]commerceRow{}
		}
		id, _ := r.integer("id")
		d.refs[table][id] = r
	}
	add("users", map[string]any{"id": 7})
	add("users", map[string]any{"id": 8})
	add("user_subscriptions", map[string]any{"id": 10, "user_id": 8, "plan_id": 5})
	add("subscription_plans", map[string]any{"id": 5})
	add("blind_box_open_records", map[string]any{"id": 61, "user_id": 7})
	add("subscription_orders", map[string]any{"id": 1, "user_id": 7, "trade_no": "legacy-sub-1"})
	for index, contract := range entitlementContracts {
		v := map[string]any{}
		for _, spec := range strings.Fields(contract.spec) {
			parts := strings.Split(spec, ":")
			switch parts[2] {
			case "i", "c", "T":
				v[parts[1]] = int64(0)
			case "n":
				v[parts[1]] = json.Number("0.123456789123456789")
			case "s":
				v[parts[1]] = ""
			case "t":
				v[parts[1]] = int64(1700000000)
			}
		}
		v["id"] = int64(101 + index)
		for name, value := range map[string]any{"user_id": 8, "user_subscription_id": 10, "plan_id": 5, "draw_id": 103,
			"reward_id": 104, "card_code": "card-10", "lucky_suffix": "0007", "winning_number": "1007", "lucky_number": "0007",
			"draw_date": "2023-11-14", "membership_tier": "pro", "timezone": "Asia/Shanghai", "status": "completed",
			"credit_status": "credited", "participation_type": "subscription", "idempotency_key": "benefit-10",
			"benefit_cycle": "2023-11", "request_id": "conversion-10", "version": "unified_credit_v1", "group_name": "default", "rule_version": "v1"} {
			if _, exists := v[name]; exists {
				v[name] = value
			}
		}
		switch contract.source {
		case "blind_box_daily_lucky_numbers":
			v["blind_box_open_record_id"], v["user_id"] = 61, 7
		case "subscription_lucky_rewards":
			v["final_reward_quota"] = int64(123456789)
		case "subscription_blind_box_benefit_cycles":
			v["ends_at"] = int64(1702592000)
			v["expected_count"], v["granted_count"] = 1, 1
		case "subscription_reset_opportunity_accounts":
			v["earned_total"], v["used_total"], v["available_total"], v["last_used_month"] = 1, 1, 0, "2023-11"
		case "subscription_reset_opportunity_ledgers":
			v["related_user_id"], v["change_type"], v["delta"], v["balance_after"] = 7, "earn", 1, 1
			v["source_type"], v["source_ref"], v["event_key"] = "subscription_order", "legacy-sub-1", "earn-7"
		case "referral_purchase_rewards":
			v["inviter_id"], v["invitee_id"], v["bonus_quota_amount"] = 8, 7, int64(55)
			v["purchase_type"], v["order_source_type"], v["order_source_id"] = "month_card", "subscription_order", "legacy-sub-1"
		case "subscription_claude_conversions":
			v["ratio_numerator"], v["ratio_denominator"], v["conversion_percent"] = 1, 10, 20
			v["source_quota"], v["target_claude_quota"] = int64(100), int64(5)
		}
		add(contract.source, v)
	}
	add("subscription_reset_opportunity_ledgers", map[string]any{"id": 114, "user_id": 8, "related_user_id": 10, "change_type": "use", "delta": -1, "balance_after": 0,
		"used_month": "2023-11", "source_type": "user_subscription", "source_ref": "10", "event_key": "use-8-2023-11", "note": "used", "created_at": 1700000001, "updated_at": 1700000001})
	return d
}

func TestEntitlementsTypedPrecisionAndPolymorphicRelation(t *testing.T) {
	d := entitlementFixture(t)
	r := Report{}
	d.validate(&r)
	if len(r.Issues) > 0 {
		t.Fatalf("fixture invalid: %+v", r.Issues)
	}
	if r.Amounts["subscription_lucky_rewards.final_reward_credits"] != "246913578" || r.Counts["subscription_reset_opportunity_ledgers"] != 2 {
		t.Fatalf("count or exact monetary sum incorrect: %+v", r)
	}
	fields, err := entitlementProject(entitlementContracts[7], d.rows["subscription_reset_opportunity_ledgers"][1])
	if err != nil || fields["related_user_id"] != nil || fields["related_subscription_id"] != int64(10) {
		t.Fatalf("polymorphic source relation incorrectly migrated: %+v %v", fields, err)
	}
	fields, err = entitlementProject(entitlementContracts[2], d.rows["subscription_lucky_draws"][0])
	if err != nil || fields["base_reward_1_usd"] != "0.123456789123456789" {
		t.Fatalf("numeric precision changed: %+v %v", fields, err)
	}
}

func TestEntitlementsRejectPendingMismatchOverflowAndUnknownParticipant(t *testing.T) {
	for _, test := range []struct{ table, field, value string }{
		{"subscription_lucky_draws", "status", `"pending"`},
		{"subscription_lucky_rewards", "credit_status", `"failed"`},
		{"subscription_blind_box_benefit_cycles", "status", `"pending"`},
		{"subscription_lucky_rewards", "participation_type", `"unknown"`},
		{"subscription_lucky_rewards", "user_id", `7`},
		{"subscription_reset_opportunity_accounts", "available_total", `2`},
		{"subscription_reset_opportunity_accounts", "last_used_month", `"2023-10"`},
		{"subscription_lucky_rewards", "final_reward_quota", fmt.Sprint(math.MaxInt64/2 + 1)},
	} {
		t.Run(test.table+test.field, func(t *testing.T) {
			d := entitlementFixture(t)
			d.rows[test.table][0][test.field] = json.RawMessage(test.value)
			r := Report{}
			d.validate(&r)
			if len(r.Issues) == 0 {
				t.Fatal("invalid entitlement accepted")
			}
		})
	}
}

func TestEntitlementsBigintBoundaryRemainsExact(t *testing.T) {
	d := entitlementFixture(t)
	row := d.rows["subscription_lucky_rewards"][0]
	row["final_reward_quota"] = json.RawMessage(fmt.Sprint(math.MaxInt64 / 2))
	r := Report{}
	d.validate(&r)
	if len(r.Issues) != 0 || r.Amounts["subscription_lucky_rewards.final_reward_credits"] != fmt.Sprint(int64(math.MaxInt64-1)) {
		t.Fatalf("bigint boundary corrupted: %+v", r)
	}
}

func TestEntitlementsExcludeOnceOnlyAuditsWithoutMoneyConversionOrBlockers(t *testing.T) {
	d := &entitlementsData{rows: map[string][]commerceRow{
		"unified_credit_user_migrations":        {{"id": json.RawMessage(`0`), "user_id": json.RawMessage(`999999`), "status": json.RawMessage(`"unknown-legacy-status"`), "legacy_gpt_quota": json.RawMessage(fmt.Sprint(int64(math.MaxInt64)))}},
		"subscription_tier_settlements":         {{"id": json.RawMessage(`0`), "user_subscription_id": json.RawMessage(`999999`), "amount_total": json.RawMessage(`-15`), "settlement_quota": json.RawMessage(fmt.Sprint(int64(math.MaxInt64))), "amount_used": json.RawMessage(`"unparseable-old-audit"`)}},
		"unified_credit_group_ratio_migrations": {{"id": json.RawMessage(`0`), "ratio_before": json.RawMessage(`"obsolete-not-a-number"`)}},
	}}
	r := Report{}
	d.validate(&r)
	if len(r.Issues) != 0 || r.Counts["retired_features.unified_credit_user_migrations"] != 1 ||
		r.Counts["retired_features.subscription_tier_settlements"] != 1 || r.Counts["retired_features.unified_credit_group_ratio_migrations"] != 1 ||
		r.Amounts["retired_features.unified_credit_user_migrations.legacy_gpt_quota_v2_units"] != fmt.Sprint(int64(math.MaxInt64)) ||
		r.Amounts["retired_features.subscription_tier_settlements.amount_total_v2_units"] != "-15" ||
		r.Counts["retired_features.subscription_tier_settlements.amount_used_unparseable"] != 1 ||
		r.Counts["unified_credit_user_migrations"] != 0 {
		t.Fatalf("excluded historical audits blocked or monetized: %+v", r)
	}
}
