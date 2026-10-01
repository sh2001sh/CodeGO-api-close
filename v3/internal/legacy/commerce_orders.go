package legacy

import (
	"fmt"
	"math"
	"strings"
	"time"
)

func commerceOrderState(state string) (string, error) {
	switch state {
	case "pending":
		return "created", nil
	case "success":
		return "paid", nil
	case "failed", "expired", "refunded", "canceled":
		return state, nil
	default:
		return "", fmt.Errorf("unknown order status")
	}
}

func (d *commerceData) projectOrder(name string, r commerceRow) (commerceProjection, error) {
	values, err := commerceFields(r, `legacy_id:id:i user_id:user_id:i trade_no:trade_no:s payment_method:payment_method:s provider:payment_provider:s
		created_at:create_time:t paid_at:complete_time:T provider_payload:provider_payload:s original_money:original_money:n
		discount_applied:first_purchase_discount_applied:b discount_multiplier:first_purchase_discount_multiplier:n
		discount_starts_at:first_purchase_discount_start_at:T discount_ends_at:first_purchase_discount_end_at:T
		refund_state:refund_status:s refund_no:refund_no:s refund_provider_id:refund_provider_id:s refund_credits:refund_quota:c refund_updated_at:refund_updated_at:T
		money:money:n refund_amount:refund_amount:n`)
	if err != nil {
		return commerceProjection{}, err
	}
	kind, currency := "topup", "cny"
	if values["provider"] == "" {
		method := values["payment_method"].(string)
		switch method {
		case "stripe", "creem", "waffo", "waffo_pancake", "xunhu":
			values["provider"] = method
		default:
			values["provider"] = "epay"
		}
	}
	if name == "subscription_orders" {
		kind = "subscription"
		planID, readErr := r.integer("plan_id")
		if readErr != nil || d.plans[planID] == nil {
			return commerceProjection{}, fmt.Errorf("referenced subscription plan is missing")
		}
		plan := d.plans[planID]
		planProjection, projectErr := d.projectPlan(plan)
		if projectErr != nil {
			return commerceProjection{}, projectErr
		}
		for _, field := range []string{"group_buy_enabled", "group_buy_target", "group_buy_bonus", "group_buy_lifetime_seconds", "group_buy_bonus2_micro", "group_buy_bonus3_micro", "group_buy_bonus5_micro"} {
			values[field] = planProjection.values[field]
		}
		currency, _ = plan.text("currency")
		if currency == "" {
			currency = "usd"
		}
		values["plan_id"] = planID
		values["credits"], err = commerceUnits(plan, "total_amount")
		if err != nil {
			return commerceProjection{}, err
		}
		values["period_seconds"], err = commerceDuration(plan)
		if err != nil {
			return commerceProjection{}, err
		}
		values["period_credits"], err = commerceUnits(plan, "period_amount")
		if err != nil {
			return commerceProjection{}, err
		}
		duration, err := commerceFields(plan, `duration_unit:duration_unit:s duration_value:duration_value:i custom_seconds:custom_seconds:i`)
		if err != nil {
			return commerceProjection{}, err
		}
		for field, value := range duration {
			values[field] = value
		}
		if values["duration_unit"] == "" {
			values["duration_unit"] = "month"
		}
		if values["duration_value"].(int64) == 0 {
			values["duration_value"] = int64(1)
		}
		reset, _ := plan.text("quota_reset_period")
		if reset == "" {
			reset = "never"
		}
		values["reset_period"] = reset
		values["reset_custom_seconds"], err = plan.integer("quota_reset_custom_seconds")
		if err != nil {
			return commerceProjection{}, err
		}
		values["legacy_periodic"] = reset != "never" && values["period_credits"].(int64) == 0
		more, err := commerceFields(r, `purchase_type:purchase_type:s group_buy_id:group_buy_id:i target_subscription_id:target_subscription_id:i
			fuel_credits:fuel_quota:c fuel_unit_price:fuel_unit_price:n fuel_expires_at:fuel_expires_at:T fulfillment_state:fulfillment_status:s`)
		if err != nil {
			return commerceProjection{}, err
		}
		for field, value := range more {
			values[field] = value
		}
		if values["fulfillment_state"] == "" {
			values["fulfillment_state"] = "completed"
		}
		if values["purchase_type"] == "" {
			values["purchase_type"] = "normal"
		}
		if values["purchase_type"] == "subscription_fuel" {
			values["purchase_type"] = "fuel"
			values["credits"] = values["fuel_credits"]
		}
	} else {
		amount, err := r.integer("amount")
		if err != nil || amount <= 0 || amount > math.MaxInt64/1_000_000 {
			return commerceProjection{}, fmt.Errorf("topup amount must be positive display credits within bigint range")
		}
		values["credits"] = amount * 1_000_000
		values["period_seconds"] = int64(0)
		values["wallet_type"], err = r.text("wallet_type")
		if err != nil {
			return commerceProjection{}, err
		}
		switch values["provider"] {
		case "stripe", "creem":
			currency = "usd"
		case "waffo":
			currency = "usd"
			if configured := d.options["WaffoCurrency"]; configured != "" {
				currency = configured
			}
		case "waffo_pancake":
			currency = "usd"
			if configured := d.options["WaffoPancakeCurrency"]; configured != "" {
				currency = configured
			}
		}
	}
	values["currency"], values["kind"] = strings.ToLower(currency), kind
	values["id"], err = commerceOrderID(values["legacy_id"].(int64), kind)
	if err != nil {
		return commerceProjection{}, err
	}
	values["amount_minor"], err = commerceMinor(r, "money", currency)
	if err != nil {
		return commerceProjection{}, err
	}
	values["refund_minor"], err = commerceMinor(r, "refund_amount", currency)
	if err != nil {
		return commerceProjection{}, err
	}
	status, err := r.text("status")
	if err != nil {
		return commerceProjection{}, err
	}
	values["state"], err = commerceOrderState(status)
	if err != nil {
		return commerceProjection{}, err
	}
	if values["refund_state"] == "success" {
		values["state"] = "refunded"
		values["refunded_at"] = values["refund_updated_at"]
	}
	if values["trade_no"] == "" || (values["state"] == "paid" && values["paid_at"] == nil) {
		return commerceProjection{}, fmt.Errorf("trade number and payment completion time required")
	}
	if err = commercePaymentIdentity(name, r, values); err != nil {
		return commerceProjection{}, err
	}
	// v2 pending orders have no expires_at. Preserve them for reconciliation,
	// without pretending their provider confirmation is an unpaid new order.
	values["expires_at"] = values["created_at"].(time.Time).Add(30 * time.Minute)
	return commerceProjection{"orders", values, []string{"legacy_id", "kind", "trade_no", "user_id"}}, nil
}
