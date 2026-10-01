package commerce

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

func (s *Service) insertOrder(ctx context.Context, o Order, groupID int64) (Order, error) {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		o, err = s.insertOrderTx(ctx, tx, o, groupID)
		return err
	})
	return o, err
}

func (s *Service) insertOrderTx(ctx context.Context, tx pgx.Tx, o Order, groupID int64, ignoreLimit ...bool) (Order, error) {
	if !o.Selection.validFor(o.Provider) {
		return o, ErrInvalid
	}
	err := func() error {
		if o.PlanID != nil {
			if err := s.checkPlanPurchaseLimitTx(ctx, tx, o, ignoreLimit); err != nil {
				return err
			}
		}
		if err := s.PrepareGroupCheckoutTx(ctx, tx, &o, groupID); err != nil {
			return err
		}
		if o.Kind == "subscription" && o.PurchaseType != "fuel" && len(ignoreLimit) == 0 && o.Credits > 0 {
			bonus, err := s.starterPurchaseBonus(ctx, tx, o)
			if err != nil {
				return err
			}
			o.Credits, err = o.Credits.Add(bonus)
			if err != nil {
				return err
			}
			if o.PeriodCredits > 0 {
				o.PeriodCredits, err = o.PeriodCredits.Add(bonus)
				if err != nil {
					return err
				}
			}
		}
		var err error
		o, err = scanOrder(tx.QueryRow(ctx, `INSERT INTO v3_commerce.orders
		 (user_id,plan_id,amount_minor,credits,period_seconds,currency,kind,provider,trade_no,created_at,expires_at,
		 group_buy_enabled,group_buy_target,group_buy_bonus,group_buy_lifetime_seconds,product_id,period_credits,reset_period,reset_custom_seconds,legacy_periodic,duration_unit,duration_value,custom_seconds,
		 group_buy_bonus2_micro,group_buy_bonus3_micro,group_buy_bonus5_micro,purchase_type,target_subscription_id,fuel_expires_at,checkout_selection)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30) RETURNING `+orderColumns,
			o.UserID, o.PlanID, o.AmountMinor, int64(o.Credits), o.PeriodSeconds, o.Currency, o.Kind, o.Provider, o.TradeNo, o.CreatedAt, o.ExpiresAt,
			o.GroupBuyEnabled, o.GroupBuyTarget, int64(o.GroupBuyBonus), o.GroupBuyLifetimeSeconds, o.ProductID, int64(o.PeriodCredits), o.ResetPeriod, o.ResetCustomSeconds, o.LegacyPeriodic, o.DurationUnit, o.DurationValue, o.CustomSeconds,
			int64(o.GroupBuyBonus2), int64(o.GroupBuyBonus3), int64(o.GroupBuyBonus5), o.PurchaseType, o.TargetSubscriptionID, o.FuelExpiresAt, o.Selection))
		if err != nil {
			return err
		}
		if err = s.FreezeMonthlyPurchaseBenefitsTx(ctx, tx, o); err != nil {
			return err
		}
		if err = s.SaveGroupCheckoutTx(ctx, tx, o, groupID); err != nil {
			return err
		}
		if len(ignoreLimit) == 0 {
			return s.ApplyCheckoutDiscountTx(ctx, tx, &o)
		}
		return nil
	}()
	return o, err
}

// checkPlanPurchaseLimitTx locks the user row and enforces the plan's
// max_purchase_per_user cap against existing subscriptions and in-flight
// orders, unless the caller explicitly asked to ignore the limit.
func (s *Service) checkPlanPurchaseLimitTx(ctx context.Context, tx pgx.Tx, o Order, ignoreLimit []bool) error {
	var id int64
	if err := tx.QueryRow(ctx, `SELECT id FROM v3_identity.users WHERE id=$1 FOR UPDATE`, o.UserID).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	var maximum int
	if err := tx.QueryRow(ctx, `SELECT max_purchase_per_user FROM v3_commerce.plans WHERE id=$1 AND enabled AND NOT internal_only FOR SHARE`, *o.PlanID).Scan(&maximum); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if maximum <= 0 || (len(ignoreLimit) != 0 && ignoreLimit[0]) {
		return nil
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM v3_commerce.subscriptions WHERE user_id=$1 AND plan_id=$2)
	 +(SELECT count(*) FROM v3_commerce.orders WHERE user_id=$1 AND plan_id=$2 AND state='created')`, o.UserID, *o.PlanID).Scan(&count); err != nil {
		return err
	}
	if count >= maximum {
		return ErrStateConflict
	}
	return nil
}
