package commerce

import (
	"context"
	"errors"
	"math/big"
	"strings"

	"github.com/jackc/pgx/v5"
)

func monthlyTierSeconds(tier string) int64 {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "lite":
		return 15 * 60
	case "standard":
		return 30 * 60
	case "pro":
		return 45 * 60
	case "ultra":
		return 60 * 60
	default:
		return 0
	}
}

func monthlyPlanSecondsTx(ctx context.Context, tx pgx.Tx, plan int64) (int64, error) {
	var tier, planType string
	err := tx.QueryRow(ctx, `SELECT membership_tier,plan_type FROM v3_commerce.plans WHERE id=$1 FOR SHARE`, plan).Scan(&tier, &planType)
	if err != nil || (planType != "monthly" && planType != "") {
		return 0, err
	}
	return monthlyTierSeconds(tier), nil
}

// FreezeMonthlyPurchaseBenefitsTx records that new checkouts grant no package
// multiplier time. Existing snapshots retain their previously promised benefit.
func (s *Service) FreezeMonthlyPurchaseBenefitsTx(ctx context.Context, tx pgx.Tx, o Order) error {
	if o.PolicyVersion == PolicyStandardV2 || o.Kind != "subscription" || o.PurchaseType == "fuel" || o.ID == 0 || o.PlanID == nil {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO v3_commerce.monthly_purchase_benefits(order_id,target_seconds,source_seconds,full_price_minor)
	 VALUES($1,0,0,$2)`, o.ID, o.AmountMinor)
	return err
}

// freezeImportedMonthlyPurchaseBenefitsTx preserves the promised card benefit
// for pending legacy orders imported before their original callback is served.
func (s *Service) freezeImportedMonthlyPurchaseBenefitsTx(ctx context.Context, tx pgx.Tx, o Order) error {
	if o.PolicyVersion == PolicyStandardV2 {
		return nil
	}
	if o.Kind != "subscription" || o.PurchaseType == "fuel" || o.ID == 0 || o.PlanID == nil {
		return nil
	}
	var target int64
	var err error
	if o.PlanSnapshot.ID > 0 {
		target = monthlyTierSeconds(o.PlanSnapshot.MembershipTier)
	} else {
		target, err = monthlyPlanSecondsTx(ctx, tx, *o.PlanID)
	}
	if err != nil {
		return err
	}
	var source int64
	if o.TargetSubscriptionID > 0 {
		p, e := subscriptionPlan(ctx, tx, o.TargetSubscriptionID)
		err = e
		source = monthlyTierSeconds(p.MembershipTier)
		if err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO v3_commerce.monthly_purchase_benefits(order_id,target_seconds,source_seconds,full_price_minor)
	 VALUES($1,$2,$3,$4)`, o.ID, target, source, o.AmountMinor)
	return err
}

// ApplyMonthlyPurchaseBenefitsTx follows successful fulfillment, including a
// package replacement. Fuel and payments awaiting provider review grant none.
func (s *Service) ApplyMonthlyPurchaseBenefitsTx(ctx context.Context, tx pgx.Tx, o Order) error {
	if o.PolicyVersion == PolicyStandardV2 {
		return nil
	}
	if o.Kind != "subscription" || o.PurchaseType == "fuel" || o.ID == 0 || s.cfg.MonthlyBenefits == nil {
		return nil
	}
	var target, source, fullPrice int64
	err := tx.QueryRow(ctx, `SELECT target_seconds,source_seconds,full_price_minor FROM v3_commerce.monthly_purchase_benefits WHERE order_id=$1`, o.ID).Scan(&target, &source, &fullPrice)
	if errors.Is(err, pgx.ErrNoRows) {
		// Historical paid callbacks return before fulfillment. A new payment
		// needs its native or imported immutable snapshot before granting.
		return ErrStateConflict
	}
	if err != nil || target == 0 {
		return err
	}
	var review bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_commerce.package_payment_reviews WHERE order_id=$1)`, o.ID).Scan(&review); err != nil || review {
		return err
	}
	var action, state string
	var used, remaining int64
	err = tx.QueryRow(ctx, `SELECT resolved_action,state,quoted_used,quoted_remaining FROM v3_commerce.package_checkouts WHERE order_id=$1`, o.ID).Scan(&action, &state, &used, &remaining)
	if errors.Is(err, pgx.ErrNoRows) {
		action = "subscribe"
	} else if err != nil {
		return err
	} else if state != "applied" {
		return ErrStateConflict
	}
	duration, err := monthlyPurchaseSeconds(target, source, fullPrice, o.AmountMinor, action, used, remaining)
	if err != nil || duration <= 0 {
		return err
	}
	return s.cfg.MonthlyBenefits.GrantMonthlyCardTx(ctx, tx, o.UserID, duration, rewardBenefitReference(o))
}

func monthlyPurchaseSeconds(target, source, fullPrice, paid int64, action string, used, remaining int64) (int64, error) {
	if target < 0 || source < 0 || fullPrice <= 0 || paid < 0 || used < 0 || remaining < 0 {
		return 0, ErrInvalid
	}
	value := new(big.Rat).SetInt64(target)
	switch action {
	case "subscribe":
	case "renew":
		value.Mul(value, new(big.Rat).SetFrac(big.NewInt(min(paid, fullPrice)), big.NewInt(fullPrice)))
	case "upgrade":
		minimum := target - source
		total := new(big.Int).Add(big.NewInt(used), big.NewInt(remaining))
		if total.Sign() > 0 {
			value.SetFrac(new(big.Int).Mul(big.NewInt(source), big.NewInt(used)), total)
			value.Add(value, new(big.Rat).SetInt64(minimum))
		}
	default:
		return 0, ErrInvalid
	}
	// Source grants whole seconds by truncation, including discounted renewals.
	seconds := new(big.Int).Quo(value.Num(), value.Denom())
	if !seconds.IsInt64() {
		return 0, ErrInvalid
	}
	return max(seconds.Int64(), 0), nil
}
