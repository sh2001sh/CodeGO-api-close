package legacy

import (
	"encoding/json"
	"testing"
)

func TestImportedCommerceRuntimeUsesSourceGroupRulesAndExactMoney(t *testing.T) {
	d := commerceTestData(t)
	plan := d.plans[5]
	for field, value := range map[string]string{"group_buy_enabled": "true", "group_buy_bonus2": "1.234567", "group_buy_bonus3": "2.345678", "group_buy_bonus5": "3.456789", "fuel_unit_price": "0.000001"} {
		plan[field] = json.RawMessage(value)
	}
	for _, name := range []string{"subscription_plans", "subscription_orders"} {
		row := plan
		if name == "subscription_orders" {
			row = commerceTestRow(t, `{"id":3,"user_id":7,"plan_id":5,"trade_no":"pending-group","status":"pending","purchase_type":"group_buy","money":12.12,"create_time":1700000000}`)
		}
		projection, err := d.project(name, row)
		if err != nil {
			t.Fatal(err)
		}
		for field, want := range map[string]any{"group_buy_enabled": true, "group_buy_target": int64(5), "group_buy_lifetime_seconds": int64(172800), "group_buy_bonus2_micro": int64(1234567), "group_buy_bonus3_micro": int64(2345678), "group_buy_bonus5_micro": int64(3456789), "group_buy_bonus": int64(3456789)} {
			if projection.values[field] != want {
				t.Fatalf("%s %s=%v want=%v", name, field, projection.values[field], want)
			}
		}
		if name == "subscription_plans" && projection.values["fuel_unit_price_micro"] != int64(1) {
			t.Fatal("fuel unit price lost sub-cent precision")
		}
	}
	plan["group_buy_bonus2"] = json.RawMessage(`9223372036854.775808`)
	if _, err := d.project("subscription_plans", plan); err == nil {
		t.Fatal("overflowing runtime reward accepted")
	}
}
