package commerce

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func (s *Service) ListPlans(ctx context.Context, includeDisabled bool) ([]Plan, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+planColumns+`
	    FROM v3_commerce.plans WHERE ($1 OR (enabled AND NOT internal_only)) ORDER BY id`, includeDisabled)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	plans := make([]Plan, 0)
	for rows.Next() {
		p, scanErr := scanPlan(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		plans = append(plans, p)
	}
	return plans, rows.Err()
}

func (s *Service) SavePlan(ctx context.Context, p Plan) (Plan, error) {
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
	var row pgx.Row
	if p.ID == 0 {
		row = s.pool.QueryRow(ctx, `INSERT INTO v3_commerce.plans(name,price_minor,currency,credits,period_seconds,enabled,
		    group_buy_enabled,group_buy_target,group_buy_bonus,group_buy_lifetime_seconds,
		    period_credits,reset_period,reset_custom_seconds,internal_only,max_purchase_per_user,duration_unit,duration_value,custom_seconds,
		    group_buy_bonus2_micro,group_buy_bonus3_micro,group_buy_bonus5_micro,plan_type,fuel_enabled,fuel_unit_price_micro,fuel_min_credits,fuel_credit_step,membership_tier,upgrade_group,model_limits)
		    VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29) RETURNING id`, p.Name, p.PriceMinor, p.Currency, int64(p.Credits), p.PeriodSeconds, p.Enabled,
			p.GroupBuyEnabled, p.GroupBuyTarget, int64(p.GroupBuyBonus), p.GroupBuyLifetimeSeconds,
			int64(p.PeriodCredits), p.ResetPeriod, p.ResetCustomSeconds, p.InternalOnly, p.MaxPurchasePerUser, p.DurationUnit, p.DurationValue, p.CustomSeconds,
			int64(p.GroupBuyBonus2), int64(p.GroupBuyBonus3), int64(p.GroupBuyBonus5), p.PlanType, p.FuelEnabled, p.FuelUnitPriceMicro, int64(p.FuelMinCredits), int64(p.FuelCreditStep), p.MembershipTier, p.UpgradeGroup, p.ModelLimits)
	} else {
		row = s.pool.QueryRow(ctx, `UPDATE v3_commerce.plans SET name=$2,price_minor=$3,currency=$4,credits=$5,period_seconds=$6,enabled=$7,
		    group_buy_enabled=$8,group_buy_target=$9,group_buy_bonus=$10,group_buy_lifetime_seconds=$11,
		    period_credits=$12,reset_period=$13,reset_custom_seconds=$14,internal_only=$15,max_purchase_per_user=$16,
		    duration_unit=$17,duration_value=$18,custom_seconds=$19,
		    group_buy_bonus2_micro=$20,group_buy_bonus3_micro=$21,group_buy_bonus5_micro=$22,plan_type=$23,fuel_enabled=$24,fuel_unit_price_micro=$25,fuel_min_credits=$26,fuel_credit_step=$27,membership_tier=$28,upgrade_group=$29,model_limits=$30
		    WHERE id=$1 RETURNING id`, p.ID, p.Name, p.PriceMinor, p.Currency, int64(p.Credits), p.PeriodSeconds, p.Enabled,
			p.GroupBuyEnabled, p.GroupBuyTarget, int64(p.GroupBuyBonus), p.GroupBuyLifetimeSeconds,
			int64(p.PeriodCredits), p.ResetPeriod, p.ResetCustomSeconds, p.InternalOnly, p.MaxPurchasePerUser, p.DurationUnit, p.DurationValue, p.CustomSeconds,
			int64(p.GroupBuyBonus2), int64(p.GroupBuyBonus3), int64(p.GroupBuyBonus5), p.PlanType, p.FuelEnabled, p.FuelUnitPriceMicro, int64(p.FuelMinCredits), int64(p.FuelCreditStep), p.MembershipTier, p.UpgradeGroup, p.ModelLimits)
	}
	err := row.Scan(&p.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return p, err
}

func validCurrency(currency string) bool {
	if len(currency) < 3 || len(currency) > 12 || currency[0] < 'a' || currency[0] > 'z' {
		return false
	}
	for _, r := range currency {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func (s *Service) grantSubscription(ctx context.Context, tx pgx.Tx, o Order) error {
	var lockedUser int64
	if err := tx.QueryRow(ctx, `SELECT id FROM v3_identity.users WHERE id=$1 FOR UPDATE`, o.UserID).Scan(&lockedUser); err != nil {
		return err
	}
	if o.PurchaseType == "fuel" {
		return s.applySubscriptionFuelTx(ctx, tx, o)
	}
	if handled, err := s.ApplyPackageCheckoutTx(ctx, tx, o); handled || err != nil {
		if err != nil {
			return err
		}
		return s.ApplyMonthlyPurchaseBenefitsTx(ctx, tx, o)
	}
	if o.PlanID == nil || o.PeriodSeconds <= 0 {
		return ErrInvalid
	}
	if err := s.insertNewSubscriptionTx(ctx, tx, o); err != nil {
		return err
	}
	return s.ApplyMonthlyPurchaseBenefitsTx(ctx, tx, o)
}

// insertNewSubscriptionTx creates the subscription row, its dedicated
// account and bucket, applies any upgrade-group transition, and posts the
// initial credit grant.
func (s *Service) insertNewSubscriptionTx(ctx context.Context, tx pgx.Tx, o Order) error {
	start := s.cfg.Now()
	var id, account int64
	end := durationEnd(start, o.DurationUnit, o.DurationValue, o.CustomSeconds, o.PeriodSeconds)
	resetPeriod := o.ResetPeriod
	if resetPeriod == "" {
		resetPeriod = "never"
	}
	next := nextReset(start, resetPeriod, o.ResetCustomSeconds, end)
	err := tx.QueryRow(ctx, `INSERT INTO v3_commerce.subscriptions(user_id,plan_id,order_id,starts_at,expires_at,
	    total_credits,renewable_credits,period_credits,legacy_periodic,last_reset_at,next_reset_at,reset_period,reset_custom_seconds)
	    VALUES($1,$2,NULLIF($3,0),$4,$5,$6,$6,$7,$8,$4,$9,$10,$11) RETURNING id`, o.UserID, *o.PlanID, o.ID, start, end,
		int64(o.Credits), int64(o.PeriodCredits), o.LegacyPeriodic, next, resetPeriod, o.ResetCustomSeconds).Scan(&id)
	if err != nil {
		return err
	}
	err = tx.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('subscription',$1,'subscription') RETURNING id`, id).Scan(&account)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE v3_commerce.subscriptions s SET account_id=$2,model_limits=p.model_limits,
	 source=CASE WHEN s.order_id IS NULL THEN '' ELSE 'order' END FROM v3_commerce.plans p WHERE s.id=$1 AND p.id=s.plan_id`, id, account); err != nil {
		return err
	}
	if err = s.ApplySubscriptionUpgradeGroupTx(ctx, tx, id, *o.PlanID); err != nil {
		return err
	}
	if o.ID == 0 {
		if _, err = tx.Exec(ctx, `UPDATE v3_commerce.subscriptions SET reward_operation=$2 WHERE id=$1`, id, o.TradeNo); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO v3_commerce.subscription_buckets(account_id,subscription_id,starts_at) VALUES($1,$2,$3)`, account, id, start); err != nil {
		return err
	}
	grant := o.Credits
	if o.Credits == 0 {
		grant = o.PeriodCredits
	} else if o.PeriodCredits > 0 {
		grant = min(grant, o.PeriodCredits)
	}
	_, err = s.poster.PostTx(ctx, tx, billing.Entry{AccountID: account, Amount: grant, Kind: "subscription_grant",
		OperationID: "subscription:grant:" + o.TradeNo, Metadata: map[string]any{"user_id": o.UserID, "subscription_id": id}})
	return err
}

func (s *Service) ListSubscriptions(ctx context.Context, userID int64) ([]Subscription, error) {
	rows, err := s.pool.Query(ctx, `SELECT s.id,s.user_id,s.plan_id,s.account_id,s.state,s.starts_at,s.expires_at,a.balance,
	    s.total_credits,s.used_credits+COALESCE(u.spent,0),s.period_credits,s.period_used+COALESCE(u.spent,0),
	    s.legacy_periodic,s.last_reset_at,s.next_reset_at,s.reset_period,s.reset_custom_seconds
	    FROM v3_commerce.subscriptions s JOIN v3_billing.accounts a ON a.id=s.account_id
	    LEFT JOIN LATERAL (SELECT GREATEST(-SUM(amount),0)::bigint spent FROM v3_billing.ledger_entries
	    WHERE account_id=s.account_id AND kind IN ('usage','refund')) u ON true
	    WHERE s.user_id=$1 AND s.deleted_at IS NULL ORDER BY s.id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Subscription, 0)
	for rows.Next() {
		var sub Subscription
		if err = rows.Scan(&sub.ID, &sub.UserID, &sub.PlanID, &sub.AccountID, &sub.State, &sub.StartsAt, &sub.ExpiresAt, &sub.Balance,
			&sub.TotalCredits, &sub.UsedCredits, &sub.PeriodCredits, &sub.PeriodUsed, &sub.LegacyPeriodic, &sub.LastResetAt, &sub.NextResetAt, &sub.ResetPeriod, &sub.ResetCustomSeconds); err != nil {
			return nil, err
		}
		result = append(result, sub)
	}
	return result, rows.Err()
}

// ExpireSubscriptions is safe for concurrent workers through SKIP LOCKED.
// It preserves already-spent credits and expires only a bucket's remaining funds.
func (s *Service) ExpireSubscriptions(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	count := 0
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		users, err := lockExpiredSubscriptionUsersTx(ctx, tx, s.cfg.Now(), limit)
		if err != nil || len(users) == 0 {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id,account_id FROM v3_commerce.subscriptions
		    WHERE user_id=ANY($3) AND state='active' AND expires_at<=$1 AND NOT EXISTS(SELECT 1 FROM v3_commerce.package_checkouts pc
		    WHERE pc.target_subscription_id=v3_commerce.subscriptions.id AND pc.state IN ('preparing','checkout')) AND NOT EXISTS(SELECT 1 FROM v3_commerce.subscription_operations op WHERE op.subscription_id=v3_commerce.subscriptions.id AND op.kind IN ('conversion','invalidate','delete') AND op.state='pending')
		    ORDER BY expires_at,id LIMIT $2 FOR UPDATE SKIP LOCKED`, s.cfg.Now(), limit, users)
		if err != nil {
			return err
		}
		var pending [][2]int64
		for rows.Next() {
			var item [2]int64
			if err = rows.Scan(&item[0], &item[1]); err != nil {
				rows.Close()
				return err
			}
			pending = append(pending, item)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		for _, item := range pending {
			if err = s.endSubscription(ctx, tx, item[0], item[1], "expired"); err != nil {
				if errors.Is(err, ErrFundingPending) {
					continue
				}
				return err
			}
			count++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (s *Service) cancelSubscriptionOrder(ctx context.Context, tx pgx.Tx, orderID int64) error {
	o, err := scanOrder(tx.QueryRow(ctx, `SELECT `+orderColumns+` FROM v3_commerce.orders WHERE id=$1`, orderID))
	if err != nil {
		return err
	}
	if o.PurchaseType == "fuel" {
		return s.RevokeSubscriptionFuelTx(ctx, tx, o)
	}
	var user int64
	if err = tx.QueryRow(ctx, `SELECT id FROM v3_identity.users WHERE id=$1 FOR UPDATE`, o.UserID).Scan(&user); err != nil {
		return err
	}
	var id, account int64
	err = tx.QueryRow(ctx, `SELECT id,account_id FROM v3_commerce.subscriptions WHERE order_id=$1 AND state='active' FOR UPDATE`, orderID).Scan(&id, &account)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.endSubscription(ctx, tx, id, account, "canceled")
}

func (s *Service) endSubscription(ctx context.Context, tx pgx.Tx, id, account int64, state string) error {
	if s.cfg.FundingDrain != nil {
		drained, err := s.cfg.FundingDrain.FreezeAndDrained(ctx, tx, account)
		if err != nil {
			return err
		}
		if !drained {
			return ErrFundingPending
		}
	}
	var balance credits.Micro
	var bucketOwner int64
	if err := tx.QueryRow(ctx, `SELECT balance,owner_id FROM v3_billing.accounts WHERE id=$1 FOR UPDATE`, account).Scan(&balance, &bucketOwner); err != nil {
		return err
	}
	if balance > 0 {
		operation := fmt.Sprintf("subscription:end:%d", id)
		if bucketOwner < 0 {
			operation = fmt.Sprintf("subscription:end:%d:%d", id, account)
		}
		if _, err := s.poster.PostTx(ctx, tx, billing.Entry{AccountID: account, Amount: -balance, Kind: "subscription_expire",
			OperationID: operation, Reason: state}); err != nil {
			return err
		}
	}
	tag, err := tx.Exec(ctx, `UPDATE v3_commerce.subscriptions SET state=$2,ended_at=$3 WHERE id=$1 AND state='active'`, id, state, s.cfg.Now())
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrStateConflict
	}
	return RestoreSubscriptionGroupTx(ctx, tx, id, s.cfg.Now())
}
