package commerce

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (s *Service) SavePlan(ctx context.Context, p Plan) (Plan, error) {
	if err := normalizePlanPolicy(&p); err != nil {
		return p, err
	}
	if err := s.normalizePlanMetadata(ctx, &p); err != nil {
		return p, err
	}
	if err := normalizePlanDuration(&p); err != nil {
		return p, err
	}
	if p.ResetPeriod == "" {
		p.ResetPeriod = "never"
	}
	if p.GroupBuyTarget == 0 {
		p.GroupBuyTarget = 5
	}
	if p.GroupBuyLifetimeSeconds == 0 {
		p.GroupBuyLifetimeSeconds = 172800
	}
	if strings.TrimSpace(p.Name) == "" || len(p.Name) > 200 || p.PriceMinor <= 0 || p.Credits < 0 || (p.Credits == 0 && p.PeriodCredits == 0) ||
		p.PeriodSeconds < 60 || p.PeriodSeconds > 31622400 || !validCurrency(p.Currency) ||
		p.GroupBuyTarget < 2 || p.GroupBuyTarget > 1000 || p.GroupBuyBonus < 0 || p.GroupBuyLifetimeSeconds < 60 || p.GroupBuyLifetimeSeconds > 31622400 ||
		p.PeriodCredits < 0 || p.MaxPurchasePerUser < 0 || !validReset(p.ResetPeriod, p.ResetCustomSeconds) ||
		p.GroupBuyBonus2 < 0 || p.GroupBuyBonus3 < 0 || p.GroupBuyBonus5 < 0 || p.FuelUnitPriceMicro < 0 || p.FuelMinCredits < 0 || p.FuelCreditStep < 0 ||
		(p.FuelEnabled && (p.FuelUnitPriceMicro == 0 || p.FuelMinCredits == 0 || p.FuelCreditStep == 0)) {
		return p, ErrInvalid
	}
	if p.MembershipTier != "" && p.MembershipTier != "none" && monthlyTierSeconds(p.MembershipTier) == 0 {
		return p, ErrInvalid
	}
	if err := s.validatePlanVersionUpdate(ctx, p); err != nil {
		return p, err
	}
	var row pgx.Row
	if p.ID == 0 {
		row = s.pool.QueryRow(ctx, `INSERT INTO v3_commerce.plans(name,price_minor,currency,credits,period_seconds,enabled,
		    group_buy_enabled,group_buy_target,group_buy_bonus,group_buy_lifetime_seconds,
		    period_credits,reset_period,reset_custom_seconds,internal_only,max_purchase_per_user,duration_unit,duration_value,custom_seconds,
		    group_buy_bonus2_micro,group_buy_bonus3_micro,group_buy_bonus5_micro,plan_type,fuel_enabled,fuel_unit_price_micro,fuel_min_credits,fuel_credit_step,membership_tier,upgrade_group,model_limits,lucky_draw_enabled,policy_version)
		    VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31) RETURNING id`, p.Name, p.PriceMinor, p.Currency, int64(p.Credits), p.PeriodSeconds, p.Enabled,
			p.GroupBuyEnabled, p.GroupBuyTarget, int64(p.GroupBuyBonus), p.GroupBuyLifetimeSeconds,
			int64(p.PeriodCredits), p.ResetPeriod, p.ResetCustomSeconds, p.InternalOnly, p.MaxPurchasePerUser, p.DurationUnit, p.DurationValue, p.CustomSeconds,
			int64(p.GroupBuyBonus2), int64(p.GroupBuyBonus3), int64(p.GroupBuyBonus5), p.PlanType, p.FuelEnabled, p.FuelUnitPriceMicro, int64(p.FuelMinCredits), int64(p.FuelCreditStep), p.MembershipTier, p.UpgradeGroup, p.ModelLimits, p.LuckyDrawEnabled, p.PolicyVersion)
	} else {
		row = s.pool.QueryRow(ctx, `UPDATE v3_commerce.plans SET name=$2,price_minor=$3,currency=$4,credits=$5,period_seconds=$6,enabled=$7,
		    group_buy_enabled=$8,group_buy_target=$9,group_buy_bonus=$10,group_buy_lifetime_seconds=$11,
		    period_credits=$12,reset_period=$13,reset_custom_seconds=$14,internal_only=$15,max_purchase_per_user=$16,
		    duration_unit=$17,duration_value=$18,custom_seconds=$19,
		    group_buy_bonus2_micro=$20,group_buy_bonus3_micro=$21,group_buy_bonus5_micro=$22,plan_type=$23,fuel_enabled=$24,fuel_unit_price_micro=$25,fuel_min_credits=$26,fuel_credit_step=$27,membership_tier=$28,upgrade_group=$29,model_limits=$30,lucky_draw_enabled=$31,policy_version=$32
		    WHERE id=$1 AND (policy_version=$32 OR NOT EXISTS(SELECT 1 FROM v3_commerce.orders WHERE plan_id=$1) AND NOT EXISTS(SELECT 1 FROM v3_commerce.subscriptions WHERE plan_id=$1) AND NOT EXISTS(SELECT 1 FROM v3_commerce.redemption_codes WHERE plan_id=$1)) RETURNING id`, p.ID, p.Name, p.PriceMinor, p.Currency, int64(p.Credits), p.PeriodSeconds, p.Enabled,
			p.GroupBuyEnabled, p.GroupBuyTarget, int64(p.GroupBuyBonus), p.GroupBuyLifetimeSeconds,
			int64(p.PeriodCredits), p.ResetPeriod, p.ResetCustomSeconds, p.InternalOnly, p.MaxPurchasePerUser, p.DurationUnit, p.DurationValue, p.CustomSeconds,
			int64(p.GroupBuyBonus2), int64(p.GroupBuyBonus3), int64(p.GroupBuyBonus5), p.PlanType, p.FuelEnabled, p.FuelUnitPriceMicro, int64(p.FuelMinCredits), int64(p.FuelCreditStep), p.MembershipTier, p.UpgradeGroup, p.ModelLimits, p.LuckyDrawEnabled, p.PolicyVersion)
	}
	err := row.Scan(&p.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrStateConflict
	}
	return p, err
}
