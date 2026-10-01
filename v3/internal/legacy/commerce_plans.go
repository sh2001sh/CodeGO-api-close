package legacy

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func (d *commerceData) projectPlan(r commerceRow) (commerceProjection, error) {
	values, err := commerceFields(r, `id:id:i name:title:s subtitle:subtitle:s currency:currency:s enabled:enabled:b internal_only:internal_only:b sort_order:sort_order:i
		stripe_price_id:stripe_price_id:s creem_product_id:creem_product_id:s max_purchase_per_user:max_purchase_per_user:i
		plan_type:plan_type:s membership_tier:membership_tier:s lucky_draw_enabled:lucky_draw_enabled:b blind_box_benefit_count:blind_box_benefit_count:i
		group_buy_enabled:group_buy_enabled:b group_buy_bonus2:group_buy_bonus2:n group_buy_bonus3:group_buy_bonus3:n group_buy_bonus5:group_buy_bonus5:n
		fuel_enabled:fuel_enabled:b fuel_unit_price:fuel_unit_price:n fuel_min_credits:fuel_min_quota:c fuel_credit_step:fuel_quota_step:c
		upgrade_group:upgrade_group:s credits:total_amount:c period_credits:period_amount:c model_limits:model_limits:j reset_period:quota_reset_period:s
		reset_custom_seconds:quota_reset_custom_seconds:i duration_unit:duration_unit:s duration_value:duration_value:i custom_seconds:custom_seconds:i
		created_at:created_at:t updated_at:updated_at:t price_amount:price_amount:n`)
	if err != nil {
		return commerceProjection{}, err
	}
	currency := strings.ToLower(values["currency"].(string))
	if currency == "" {
		currency = "usd"
	}
	values["currency"] = currency
	if values["duration_unit"] == "" {
		values["duration_unit"] = "month"
	}
	if values["duration_value"].(int64) == 0 {
		values["duration_value"] = int64(1)
	}
	values["price_minor"], err = commerceMinor(r, "price_amount", currency)
	if err != nil {
		return commerceProjection{}, err
	}
	values["period_seconds"], err = commerceDuration(r)
	if err != nil {
		return commerceProjection{}, err
	}
	if values["name"] == "" || values["price_minor"].(int64) <= 0 {
		return commerceProjection{}, fmt.Errorf("plan title and positive price in target minor units required")
	}
	if seconds := values["period_seconds"].(int64); seconds < 60 || seconds > 31622400 {
		return commerceProjection{}, fmt.Errorf("plan duration must be within target 60..31622400 seconds")
	}
	if values["reset_period"] == "" {
		values["reset_period"] = "never"
	}
	if err = commercePlanRuntime(values); err != nil {
		return commerceProjection{}, err
	}
	switch values["reset_period"] {
	case "never", "daily", "weekly", "monthly":
	case "custom":
		if values["reset_custom_seconds"].(int64) <= 0 {
			return commerceProjection{}, fmt.Errorf("custom reset requires positive seconds")
		}
	default:
		return commerceProjection{}, fmt.Errorf("unknown subscription reset period")
	}
	return commerceProjection{"plans", values, []string{"name", "currency"}}, nil
}

func commercePlanRuntime(values map[string]any) error {
	for _, field := range []string{"group_buy_bonus2", "group_buy_bonus3", "group_buy_bonus5", "fuel_unit_price"} {
		micro, err := decimalProduct(values[field].(string), "1000000")
		if err != nil {
			return fmt.Errorf("%s: %w", field, err)
		}
		values[field+"_micro"] = micro
	}
	// v2 creates five-member rooms that remain open for 48 hours.
	values["group_buy_target"] = int64(5)
	values["group_buy_lifetime_seconds"] = int64(48 * 60 * 60)
	values["group_buy_bonus"] = values["group_buy_bonus5_micro"]
	return nil
}

func (d *commerceData) projectSubscription(r commerceRow) (commerceProjection, error) {
	values, err := commerceFields(r, `id:id:i user_id:user_id:i plan_id:plan_id:i total_credits:amount_total:c used_credits:amount_used:c
		period_credits:period_amount:c period_used:period_used:c model_limits:model_limits:j model_usage:model_usage:j
		starts_at:start_time:t expires_at:end_time:t source:source:s last_reset_at:last_reset_time:T next_reset_at:next_reset_time:T
		upgrade_group:upgrade_group:s prev_user_group:prev_user_group:s lucky_benefit_cycle:lucky_benefit_cycle:s membership_tier:membership_tier:s
		created_at:created_at:t updated_at:updated_at:t`)
	if err != nil {
		return commerceProjection{}, err
	}
	plan := d.plans[values["plan_id"].(int64)]
	if plan == nil {
		return commerceProjection{}, fmt.Errorf("referenced subscription plan is missing")
	}
	status, err := r.text("status")
	if err != nil {
		return commerceProjection{}, err
	}
	switch status {
	case "active", "expired":
		values["state"] = status
	case "cancelled", "canceled", "settled_cancelled":
		values["state"] = "canceled"
	default:
		return commerceProjection{}, fmt.Errorf("unknown subscription status")
	}
	values["original_status"] = status
	values["reset_opportunity_used"] = d.resetUsed[values["id"].(int64)]
	total, used := values["total_credits"].(int64), values["used_credits"].(int64)
	period, periodUsed := values["period_credits"].(int64), values["period_used"].(int64)
	reset, _ := plan.text("quota_reset_period")
	if reset == "" {
		reset = "never"
	}
	values["reset_period"] = reset
	values["reset_custom_seconds"], err = plan.integer("quota_reset_custom_seconds")
	if err != nil {
		return commerceProjection{}, err
	}
	base, err := commerceUnits(plan, "total_amount")
	if err != nil {
		return commerceProjection{}, err
	}
	values["renewable_credits"] = d.subscriptionRenewable(values["id"].(int64), total, base)
	if period == 0 && reset != "" && reset != "never" && total > 0 {
		// Older subscriptions store each refreshed cycle in amount_total/used.
		period, periodUsed = total, used
		values["period_credits"], values["period_used"] = period, periodUsed
		values["legacy_periodic"] = true
	} else {
		values["legacy_periodic"] = false
		if period == 0 {
			period, err = commerceUnits(plan, "period_amount")
			if err != nil {
				return commerceProjection{}, err
			}
			values["period_credits"] = period
		}
	}
	if (total > 0 && used > total) || (period > 0 && periodUsed > period) {
		return commerceProjection{}, fmt.Errorf("subscription usage exceeds total or cycle credits")
	}
	limits, usage := map[string]int64{}, map[string]int64{}
	if err = json.Unmarshal([]byte(values["model_limits"].(string)), &limits); err != nil {
		return commerceProjection{}, err
	}
	if err = json.Unmarshal([]byte(values["model_usage"].(string)), &usage); err != nil {
		return commerceProjection{}, err
	}
	for model, amount := range usage {
		if cap := limits[model]; cap > 0 && amount > cap {
			return commerceProjection{}, fmt.Errorf("subscription model usage exceeds its limit")
		}
	}
	if !values["expires_at"].(time.Time).After(values["starts_at"].(time.Time)) {
		return commerceProjection{}, fmt.Errorf("subscription expiration must follow start")
	}
	if values["state"] != "active" {
		values["ended_at"] = values["updated_at"]
	}
	return commerceProjection{"subscriptions", values, []string{"user_id", "plan_id", "starts_at"}}, nil
}
