package commerce

import (
	"context"
	"encoding/json"
	"math/big"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

type checkoutCampaign struct {
	enabled    bool
	multiplier string
	start, end int64
}

func loadCheckoutCampaign(ctx context.Context, tx pgx.Tx) (checkoutCampaign, error) {
	c := checkoutCampaign{multiplier: "0.8"}
	rows, err := tx.Query(ctx, `SELECT key,value FROM v3_platform.settings WHERE key IN
	 ('payment_setting.first_purchase_discount_enabled','payment_setting.first_purchase_discount_multiplier',
	 'payment_setting.first_purchase_discount_start_at','payment_setting.first_purchase_discount_end_at') AND NOT sensitive`)
	if err != nil {
		return c, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var raw []byte
		if err = rows.Scan(&key, &raw); err != nil {
			return c, err
		}
		value := string(raw)
		if len(value) > 0 && value[0] == '"' {
			if err = json.Unmarshal(raw, &value); err != nil {
				return c, err
			}
		}
		switch strings.TrimPrefix(key, "payment_setting.first_purchase_discount_") {
		case "enabled":
			c.enabled, err = strconv.ParseBool(value)
		case "multiplier":
			c.multiplier = value
		case "start_at":
			c.start, err = strconv.ParseInt(value, 10, 64)
		case "end_at":
			c.end, err = strconv.ParseInt(value, 10, 64)
		}
		if err != nil {
			return c, ErrInvalid
		}
	}
	return c, rows.Err()
}

func (c checkoutCampaign) active(now int64) bool {
	m, ok := new(big.Rat).SetString(c.multiplier)
	return c.enabled && ok && m.Sign() > 0 && m.Cmp(big.NewRat(1, 1)) < 0 && c.start > 0 && c.end > c.start && now >= c.start && now <= c.end
}

func campaignEligibleTx(ctx context.Context, tx pgx.Tx, o Order) (bool, error) {
	if o.Kind != "subscription" || o.PlanID == nil || o.PurchaseType == "fuel" || o.PurchaseType == "subscription_fuel" {
		return false, nil
	}
	var monthly bool
	err := tx.QueryRow(ctx, `SELECT plan_type='monthly' AND duration_unit='month' FROM v3_commerce.plans WHERE id=$1`, *o.PlanID).Scan(&monthly)
	if err != nil || !monthly {
		return false, err
	}
	var used bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_commerce.orders o JOIN v3_commerce.plans p ON p.id=o.plan_id
	 LEFT JOIN v3_commerce.checkout_discounts d ON d.order_id=o.id
	 WHERE o.user_id=$1 AND o.id<>$2 AND p.plan_type='monthly' AND p.duration_unit='month'
	 AND o.purchase_type NOT IN('fuel','subscription_fuel') AND
	 (o.state='paid' OR (o.state='created' AND (COALESCE(d.campaign,false) OR COALESCE((to_jsonb(o)->>'discount_applied')::boolean,false)))))`, o.UserID, o.ID).Scan(&used)
	return !used, err
}

func discountMinor(amount int64, multiplier string) (int64, error) {
	m, ok := new(big.Rat).SetString(multiplier)
	if !ok || m.Sign() <= 0 || m.Cmp(big.NewRat(1, 1)) > 0 || amount <= 0 {
		return 0, ErrInvalid
	}
	value, err := roundPositiveRat(m.Mul(m, new(big.Rat).SetInt64(amount)))
	return max(value, 1), err
}
