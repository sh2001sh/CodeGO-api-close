package legacy

import (
	"encoding/json"
	"strings"
)

func marketplaceProviderPayload(value string) json.RawMessage {
	if value == "" {
		return json.RawMessage(`{}`)
	}
	if json.Valid([]byte(value)) {
		return json.RawMessage(value)
	}
	encoded, _ := json.Marshal(value)
	return encoded
}

// Explicit field lists preserve usable domain columns. They are deliberately
// not a raw JSON archive of the source rows.
func (d *marketplaceData) projectBoxRow(table string, r marketplaceSourceRow, texts, integers, units, usd, times string) map[string]any {
	fields := make(map[string]any)
	for _, key := range strings.Fields(texts) {
		fields[key] = r.text(key)
	}
	for _, key := range strings.Fields(integers) {
		fields[key] = d.integer(r, key)
	}
	for _, mapping := range strings.Fields(units) {
		source, target, ok := strings.Cut(mapping, ":")
		if !ok {
			source = mapping
			target = mapping
		}
		value, err := FromV2Units(d.integer(r, source))
		if err != nil {
			d.problem(table, r.id(), "invalid_marketplace_amount", err.Error())
		}
		fields[target] = int64(value)
	}
	for _, mapping := range strings.Fields(usd) {
		source, target, ok := strings.Cut(mapping, ":")
		if !ok {
			source = mapping
			target = mapping
		}
		fields[target] = d.money(r, source, 1000000)
	}
	for _, mapping := range strings.Fields(times) {
		source, target, ok := strings.Cut(mapping, ":")
		if !ok {
			source = mapping
			target = mapping
		}
		fields[target] = d.instant(r, source, source == "created_at" || source == "updated_at" || source == "create_time")
	}
	return fields
}

func (d *marketplaceData) normalizeBoxOrders() {
	for _, r := range d.source["blind_box_orders"] {
		fields := d.projectBoxRow("blind_box_orders", r, "trade_no payment_method payment_provider source benefit_cycle status", "user_id quantity opened_count", "", "", "create_time:created_at complete_time:completed_at expires_at")
		fields["amount_minor"] = d.money(r, "money", 100)
		var subscription any
		if sub := d.integer(r, "user_subscription_id"); sub > 0 {
			subscription = sub
		}
		fields["subscription_id"] = subscription
		poolID := int64(2)
		if d.hasOrderInventory(r) {
			poolID = 1
		}
		fields["pool_id"] = poolID
		fields["currency"] = "usd"
		provider := r.text("payment_provider")
		if provider == "" && r.text("source") == "purchase" {
			provider = r.text("payment_method")
			if provider != "stripe" && provider != "creem" && provider != "xunhu" {
				provider = "epay"
			}
			fields["payment_provider"] = provider
		}
		if provider == "epay" || provider == "xunhu" {
			fields["currency"] = "cny"
		}
		fields["provider_payload"] = marketplaceProviderPayload(r.text("provider_payload"))
		if fields["quantity"].(int64) < 1 || fields["opened_count"].(int64) < 0 || fields["opened_count"].(int64) > fields["quantity"].(int64) {
			d.problem("blind_box_order", r.id(), "invalid_blind_box_order_quantity", "opened quantity must fit the purchased quantity")
		}
		if r.text("trade_no") == "" {
			d.problem("blind_box_order", r.id(), "missing_blind_box_order_trade", "trade number required")
		}
		d.add("blind_box_orders", r.id(), fields)
	}
}

func (d *marketplaceData) normalizeBoxHistory() {
	for _, r := range d.source["blind_box_grants"] {
		d.add("blind_box_grants", r.id(), d.projectBoxRow("blind_box_grants", r, "reason idempotency_key trade_no", "user_id admin_user_id blind_box_order_id quantity", "", "", "created_at"))
	}
	// The user explicitly retired the old points system. Report its source
	// counts/units in validate; never turn old blind_box_credits into real money.
	for _, r := range d.source["blind_box_prop_gifts"] {
		d.add("blind_box_prop_gifts", r.id(), d.projectBoxRow("blind_box_prop_gifts", r, "request_id sender_external_id recipient_external_id prop_type prop_title status", "prop_id sender_user_id recipient_user_id", "", "", "created_at"))
	}
	for _, r := range d.source["blind_box_prop_discount_usages"] {
		fields := d.projectBoxRow("blind_box_prop_discount_usages", r, "request_id prop_title channel_scope model_name", "user_id prop_id channel_id", "quota_before_discount:before_micro quota_after_discount:after_micro discount_quota:discount_micro remaining_quota:remaining_micro", "", "created_at")
		for _, key := range []string{"discount_rate", "multiplier", "effective_multiplier"} {
			fields[key+"_ppm"] = d.money(r, key, 1000000)
		}
		if fields["before_micro"].(int64) < 0 || fields["after_micro"].(int64) < 0 || fields["discount_micro"].(int64) < 0 || fields["before_micro"].(int64)-fields["after_micro"].(int64) != fields["discount_micro"].(int64) {
			d.problem("blind_box_discount_usage", r.id(), "invalid_blind_box_discount", "discount must equal the difference of before and after")
		}
		d.add("blind_box_prop_discount_usages", r.id(), fields)
	}
	for _, r := range d.source["balance_blind_box_gifts"] {
		d.add("blind_box_gifts", r.id(), d.projectBoxRow("blind_box_gifts", r, "request_id sender_external_id recipient_external_id sender_display_name_masked recipient_display_name_masked status", "sender_user_id recipient_user_id quantity", "", "", "created_at"))
	}
	for _, r := range d.source["balance_blind_box_gift_items"] {
		d.add("blind_box_gift_items", r.id(), d.projectBoxRow("blind_box_gift_items", r, "", "gift_id item_id from_user_id to_user_id", "", "", "created_at"))
	}
	for _, r := range d.source["blind_box_zero_hour_states"] {
		d.add("blind_box_zero_hour_states", r.id(), d.projectBoxRow("blind_box_zero_hour_states", r, "", "user_id points hit_count", "usage_quota:usage_micro", "", "updated_at"))
	}
	d.validateBoxReferences()
}

func (d *marketplaceData) validateBoxReferences() {
	users := marketplaceIndex(d.source["users"])
	refs := map[string]map[int64]marketplaceSourceRow{
		"user_id": users, "owner_user_id": users, "sender_user_id": users, "recipient_user_id": users, "from_user_id": users, "to_user_id": users, "admin_user_id": users,
		"prop_id": marketplaceIndex(d.source["blind_box_props"]), "open_record_id": marketplaceIndex(d.source["blind_box_open_records"]), "blind_box_order_id": marketplaceIndex(d.source["blind_box_orders"]),
	}
	for _, r := range d.records {
		for key, index := range refs {
			value, ok := r.Fields[key].(int64)
			if ok && value > 0 && index[value] == nil {
				d.problem(r.Table, r.ID, "missing_marketplace_reference", key+" has no matching source row")
			}
		}
	}
}
