package legacy

import (
	"encoding/json"
	"testing"
)

func TestSampledOpeningPreservesAssetsAndRejectsUnpaidReward(t *testing.T) {
	for _, incomplete := range []bool{false, true} {
		d := entitlementFixture(t)
		d.sampled = true
		if incomplete {
			d.rows["subscription_lucky_rewards"][0]["credit_status"] = json.RawMessage(`"pending"`)
		}
		for table, rows := range d.rows {
			var keep []commerceRow
			for _, row := range rows {
				if !preserveCompletedEntitlementHistory(table, row) {
					keep = append(keep, row)
				}
			}
			d.rows[table] = keep
		}
		r := Report{}
		d.validate(&r)
		if incomplete && len(r.Issues) == 0 {
			t.Fatal("pending reward disappeared from the financial drain")
		}
		if !incomplete && len(r.Issues) != 0 {
			t.Fatalf("completed history blocked startup: %+v", r.Issues)
		}
		if len(d.rows["subscription_reset_opportunity_accounts"]) != 1 || len(d.rows["blind_box_daily_lucky_numbers"]) != 1 || len(d.rows["subscription_claude_conversions"]) != 1 {
			t.Fatal("current assets or conversion facts removed")
		}
		account := d.rows["subscription_reset_opportunity_accounts"][0]
		account["available_total"] = json.RawMessage(`-1`)
		r = Report{}
		d.validate(&r)
		if len(r.Issues) == 0 {
			t.Fatal("invalid current asset balance accepted")
		}
	}
}

func TestIncompleteBoxBenefitIsNotHistorical(t *testing.T) {
	d := entitlementFixture(t)
	row := d.rows["subscription_blind_box_benefit_cycles"][0]
	row["granted_count"] = json.RawMessage(`0`)
	if preserveCompletedEntitlementHistory("subscription_blind_box_benefit_cycles", row) {
		t.Fatal("unfulfilled box grant was discarded")
	}
}
