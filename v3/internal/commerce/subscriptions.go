package commerce

import (
	"context"
	"errors"
	"fmt"

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
	if o.PlanSnapshot.ID == 0 {
		p, err := scanPlan(tx.QueryRow(ctx, `SELECT `+planColumns+` FROM v3_commerce.plans WHERE id=$1`, *o.PlanID))
		if err != nil {
			return err
		}
		o.PlanSnapshot = p
	}
	if o.ID == 0 && o.RecognizedRevenueCredits == nil {
		zero := credits.Micro(0)
		o.RecognizedRevenueCredits = &zero
	}
	start := s.cfg.Now()
	var id, account int64
	end := durationEnd(start, o.DurationUnit, o.DurationValue, o.CustomSeconds, o.PeriodSeconds)
	resetPeriod := o.ResetPeriod
	if resetPeriod == "" {
		resetPeriod = "never"
	}
	next := nextReset(start, resetPeriod, o.ResetCustomSeconds, end)
	err := tx.QueryRow(ctx, `INSERT INTO v3_commerce.subscriptions(user_id,plan_id,order_id,starts_at,expires_at,
	    total_credits,renewable_credits,period_credits,legacy_periodic,last_reset_at,next_reset_at,reset_period,reset_custom_seconds,policy_version,plan_snapshot,recognized_revenue_credits)
	    VALUES($1,$2,NULLIF($3,0),$4,$5,$6,$6,$7,$8,$4,$9,$10,$11,$12,$13,$14) RETURNING id`, o.UserID, *o.PlanID, o.ID, start, end,
		int64(o.Credits), int64(o.PeriodCredits), o.LegacyPeriodic, next, resetPeriod, o.ResetCustomSeconds, policyVersion(o.PolicyVersion), o.PlanSnapshot, o.RecognizedRevenueCredits).Scan(&id)
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
	if _, err = tx.Exec(ctx, `UPDATE v3_commerce.subscriptions SET model_limits=COALESCE($2::jsonb,'{}'::jsonb) WHERE id=$1`, id, o.PlanSnapshot.ModelLimits); err != nil {
		return err
	}
	if err = applySubscriptionGroupTx(ctx, tx, id, o.PlanSnapshot.UpgradeGroup, o.PlanID, s.cfg.Now()); err != nil {
		return err
	}
	if o.ID == 0 {
		if _, err = tx.Exec(ctx, `UPDATE v3_commerce.subscriptions SET reward_operation=$2 WHERE id=$1`, id, o.TradeNo); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO v3_commerce.subscription_buckets(account_id,subscription_id,starts_at,policy_version) VALUES($1,$2,$3,$4)`, account, id, start, policyVersion(o.PolicyVersion)); err != nil {
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
	if err != nil {
		return err
	}
	return s.applySubscriptionGrantedTx(ctx, tx, o.UserID)
}

func (s *Service) ListSubscriptions(ctx context.Context, userID int64) ([]Subscription, error) {
	rows, err := s.pool.Query(ctx, `SELECT s.id,s.user_id,s.plan_id,s.account_id,s.state,s.starts_at,s.expires_at,a.balance,
	    s.total_credits,s.used_credits+COALESCE(u.spent,0),s.period_credits,s.period_used+COALESCE(u.spent,0),
	    s.legacy_periodic,s.last_reset_at,s.next_reset_at,s.reset_period,s.reset_custom_seconds,s.policy_version,s.converted_at,s.benefits_until,s.plan_snapshot
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
			&sub.TotalCredits, &sub.UsedCredits, &sub.PeriodCredits, &sub.PeriodUsed, &sub.LegacyPeriodic, &sub.LastResetAt, &sub.NextResetAt, &sub.ResetPeriod, &sub.ResetCustomSeconds, &sub.PolicyVersion, &sub.ConvertedAt, &sub.BenefitsUntil, &sub.PlanSnapshot); err != nil {
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
	count, err := s.expireConvertedBenefits(ctx, limit)
	if err != nil {
		return 0, err
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
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
	if handled, e := s.refundConvertedSubscriptionTx(ctx, tx, o); handled || e != nil {
		return e
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
