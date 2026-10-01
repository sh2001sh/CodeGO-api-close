package legacy

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Derived checkout rows are required before accepting a payment callback. A
// source-row check alone cannot detect a missing or changed frozen intent.
func (m *Importer) checkCommerceRuntime(ctx context.Context, tx pgx.Tx, d *commerceData, report *Report) error {
	if report.Counts == nil {
		report.Counts = map[string]int64{}
	}
	for _, name := range []string{"top_ups", "subscription_orders"} {
		for _, source := range d.rows[name] {
			order, err := d.projectOrder(name, source)
			if err != nil {
				return err
			}
			v := order.values
			id := v["id"].(int64)
			if name == "subscription_orders" {
				kind, state := v["purchase_type"].(string), v["state"].(string)
				if state == "created" && kind != "fuel" {
					plan := d.plans[v["plan_id"].(int64)]
					price, err := commerceMinor(plan, "price_amount", v["currency"].(string))
					if err != nil {
						return err
					}
					var sourceSeconds int64
					if target := v["target_subscription_id"].(int64); target > 0 {
						for _, subscription := range d.rows["user_subscriptions"] {
							subID, _ := subscription.integer("id")
							user, _ := subscription.integer("user_id")
							if subID == target && user == v["user_id"].(int64) {
								planID, _ := subscription.integer("plan_id")
								sourceSeconds = commerceMonthlySeconds(d.plans[planID])
							}
						}
					}
					fields := map[string]any{"order_id": id, "target_seconds": commerceMonthlySeconds(plan), "source_seconds": sourceSeconds, "full_price_minor": price}
					if err := checkCommerceDerived(ctx, tx, report, "monthly_purchase_benefits", id, fields); err != nil {
						return err
					}
				}
				if (kind == "group_buy" || kind == "join_group") && (state == "created" || state == "expired" || state == "canceled" || state == "failed") {
					var group any
					if kind == "join_group" {
						group = v["group_buy_id"]
					}
					fields := map[string]any{"order_id": id, "user_id": v["user_id"], "purchase_type": kind, "requested_group_id": group, "state": "pending"}
					if err := checkCommerceDerived(ctx, tx, report, "group_checkouts", id, fields); err != nil {
						return err
					}
				}
			}
			if err := checkCommerceDiscount(ctx, tx, report, order); err != nil {
				return err
			}
		}
	}
	return nil
}

func commerceMonthlySeconds(plan commerceRow) int64 {
	kind, _ := plan.text("plan_type")
	if kind != "" && kind != "monthly" {
		return 0
	}
	tier, _ := plan.text("membership_tier")
	return map[string]int64{"lite": 900, "standard": 1800, "pro": 2700, "ultra": 3600}[strings.ToLower(strings.TrimSpace(tier))]
}

func checkCommerceDerived(ctx context.Context, tx pgx.Tx, report *Report, table string, id int64, fields map[string]any) error {
	matches, err := checkProjection(ctx, tx, "v3_commerce."+table, fields)
	if err != nil {
		return fmt.Errorf("legacy: check derived %s: %w", table, err)
	}
	if !matches {
		checkIssue(report, table, id, "missing or changed frozen checkout metadata")
	}
	report.Counts["check:commerce_runtime"]++
	return nil
}

func checkCommerceDiscount(ctx context.Context, tx pgx.Tx, report *Report, order commerceProjection) error {
	v := order.values
	if v["purchase_type"] == "fuel" {
		return nil
	}
	var count, prop, rate int64
	if err := tx.QueryRow(ctx, `SELECT count(*),COALESCE(min(id),0),COALESCE(min(discount_rate_ppm),0)
	 FROM v3_marketplace.blind_box_props WHERE user_id=$1 AND reserved_order_type=$2 AND reserved_order_trade_no=$3 AND status IN('reserved','used')`, v["user_id"], v["kind"], v["trade_no"]).Scan(&count, &prop, &rate); err != nil {
		return err
	}
	campaign := v["discount_applied"].(bool)
	if !campaign && count == 0 {
		return nil
	}
	id := v["id"].(int64)
	if count > 1 || (campaign && count != 0) {
		checkIssue(report, "checkout_discounts", id, "ambiguous source campaign or card binding")
		return nil
	}
	multiplier := v["discount_multiplier"].(string)
	var propID any
	if !campaign {
		propID = prop
		if rate == 1000000 {
			rate = 990000
		}
		multiplier = new(big.Rat).SetFrac(big.NewInt(1000000-rate), big.NewInt(1000000)).FloatString(6)
	}
	original := v["amount_minor"].(int64)
	known := false
	amount, ok := new(big.Rat).SetString(v["original_money"].(string))
	if !ok {
		return fmt.Errorf("legacy: invalid original checkout amount")
	}
	if amount.Sign() > 0 {
		minor, err := commerceMinor(commerceRow{"original": []byte(v["original_money"].(string))}, "original", v["currency"].(string))
		if err != nil {
			return err
		}
		original, known = minor, true
	}
	state := "reserved"
	switch v["state"] {
	case "paid", "refunded":
		state = "consumed"
	case "expired", "failed", "canceled":
		if campaign {
			state = "released"
		}
	}
	stamp := func(value any) int64 {
		if value == nil {
			return 0
		}
		return value.(time.Time).Unix()
	}
	var matches bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_commerce.checkout_discounts WHERE order_id=$1
	 AND original_minor=$2 AND original_known=$3 AND paid_minor=$4 AND campaign=$5 AND multiplier::numeric=$6::numeric
	 AND starts_at=$7 AND ends_at=$8 AND prop_id IS NOT DISTINCT FROM $9::bigint AND state=$10)`, id, original, known,
		v["amount_minor"], campaign, multiplier, stamp(v["discount_starts_at"]), stamp(v["discount_ends_at"]), propID, state).Scan(&matches)
	if err != nil {
		return err
	}
	if !matches {
		checkIssue(report, "checkout_discounts", id, "missing or changed original price, discount or card binding")
	}
	report.Counts["check:commerce_runtime"]++
	return nil
}
