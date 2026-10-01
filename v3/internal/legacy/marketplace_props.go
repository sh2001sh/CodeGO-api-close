package legacy

import "fmt"

func (d *marketplaceData) propReward(r marketplaceSourceRow) (map[string]any, error) {
	base := map[string]any{"kind": "multiplier", "title": r.text("title"), "weight": int64(1), "multiplier_ppm": d.money(r, "multiplier", 1000000), "duration_seconds": d.integer(r, "duration_seconds")}
	switch r.text("prop_type") {
	case "consume_discount_95", "consume_discount_90", "consume_discount_10", "zero_hour_multiplier", "monthly_pass_multiplier":
		// Historical active/expired cards may store their time only in expiry.
		// The target keeps zero duration, remaining time and expiry independently.
	case "subscription":
		base["kind"] = "subscription"
		base["plan_id"] = d.integer(r, "plan_id")
		if d.integer(r, "plan_id") <= 0 {
			return base, fmt.Errorf("subscription prop requires plan_id")
		}
	case "topup_discount_90":
		base["kind"] = "topup_discount"
		base["discount_rate_ppm"] = d.money(r, "discount_rate", 1000000)
	case "subscription_discount_90":
		base["kind"] = "subscription_discount"
		base["discount_rate_ppm"] = d.money(r, "discount_rate", 1000000)
	case "extra_draw":
		base["kind"] = "extra_draw"
	default:
		return base, fmt.Errorf("unknown prop type %q", r.text("prop_type"))
	}
	base["prop_type"] = r.text("prop_type")
	maxValue, e := FromV2Units(d.integer(r, "max_discount_quota"))
	if e != nil {
		return base, e
	}
	used, e := FromV2Units(d.integer(r, "used_discount_quota"))
	if e != nil {
		return base, e
	}
	base["max_discount_micro"] = int64(maxValue)
	base["used_discount_micro"] = int64(used)
	return base, nil
}

func (d *marketplaceData) normalizeProps() {
	opens := marketplaceIndex(d.source["blind_box_open_records"])
	for _, r := range d.source["blind_box_props"] {
		id := r.id()
		reward, err := d.propReward(r)
		if err != nil {
			d.problem("blind_box_prop", id, "unsupported_blind_box_prop", err.Error())
		}
		kind, _ := reward["kind"].(string)
		status := r.text("status")
		if status != "available" && status != "active" && status != "paused" && status != "used" && status != "expired" && status != "reserved" {
			d.problem("blind_box_prop", id, "invalid_blind_box_prop_state", "target cannot represent this prop state")
		}
		user, openID := d.integer(r, "user_id"), d.integer(r, "open_record_id")
		if openID > 0 && opens[openID] == nil {
			d.problem("blind_box_prop", id, "missing_blind_box_prop_open", "source open record does not exist")
		}
		factor := d.money(r, "multiplier", 1000000)
		duration, remaining := d.integer(r, "duration_seconds"), d.integer(r, "remaining_seconds")
		if factor > 1000000 || duration < 0 || remaining < 0 {
			d.problem("blind_box_prop", id, "invalid_blind_box_multiplier", "factor or duration outside allowed range")
		}
		var openReference any
		if openID > 0 {
			openReference = openID
		}
		fields := map[string]any{"user_id": user, "open_record_id": openReference, "multiplier_ppm": factor, "duration_seconds": duration, "remaining_seconds": remaining, "kind": kind, "title": r.text("title"), "status": status, "started_at": d.instant(r, "activated_at", false), "expires_at": d.instant(r, "expires_at", false), "created_at": d.instant(r, "created_at", true), "updated_at": d.instant(r, "updated_at", true)}
		fields["prop_type"] = r.text("prop_type")
		fields["discount_rate_ppm"] = d.money(r, "discount_rate", 1000000)
		fields["max_discount_micro"] = reward["max_discount_micro"]
		fields["used_discount_micro"] = reward["used_discount_micro"]
		fields["reserved_at"] = d.instant(r, "reserved_at", false)
		fields["used_at"] = d.instant(r, "used_at", false)
		fields["reserved_order_type"] = r.text("reserved_order_type")
		fields["reserved_order_trade_no"] = r.text("reserved_order_trade_no")
		fields["benefit_reference"] = r.text("benefit_reference")
		if maxValue, ok := fields["max_discount_micro"].(int64); ok {
			used, _ := fields["used_discount_micro"].(int64)
			if maxValue < 0 || used < 0 || (maxValue > 0 && used > maxValue && !marketplaceLegacyUncappedProp(r.text("prop_type"))) {
				d.problem("blind_box_prop", id, "invalid_blind_box_discount_budget", "used discount outside card budget")
			}
		}
		if kind == "subscription" {
			fields["plan_id"] = d.integer(r, "plan_id")
		}
		d.add("blind_box_props", id, fields)
	}
}

func marketplaceLegacyUncappedProp(kind string) bool {
	switch kind {
	case "consume_discount_95", "consume_discount_90", "consume_discount_10", "zero_hour_multiplier", "monthly_pass_multiplier":
		return true
	default:
		return false
	}
}
