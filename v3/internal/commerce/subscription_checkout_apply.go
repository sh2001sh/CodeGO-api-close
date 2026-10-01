package commerce

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// ApplyPackageCheckoutTx shares the verified payment receipt's transaction.
// A restored/canceled quote cannot grant its discount after spending resumed.
// Such a late external payment requires provider reconciliation, not free quota.
func (s *Service) ApplyPackageCheckoutTx(ctx context.Context, tx pgx.Tx, o Order) (bool, error) {
	checkout, found, err := loadPackageCheckoutForApplyTx(ctx, tx, o.ID)
	if err != nil {
		return true, err
	}
	if !found {
		if o.TargetSubscriptionID > 0 {
			return true, s.recordPackagePaymentReview(ctx, tx, o, "targeted payment has no frozen package checkout intent")
		}
		return false, nil
	}
	if checkout.state == "applied" {
		return true, nil
	}
	if checkout.state != "checkout" {
		return true, s.recordPackagePaymentReview(ctx, tx, o, "payment arrived after quote restoration")
	}
	sub, ok, err := s.verifyPackageCheckoutSubscriptionTx(ctx, tx, checkout, o)
	if err != nil {
		return true, err
	}
	if !ok {
		return true, s.recordPackagePaymentReview(ctx, tx, o, "quoted subscription changed before payment confirmation")
	}
	plan, review, err := s.quotePackageCheckoutGrantTx(ctx, tx, checkout, sub, o)
	if err != nil {
		return true, err
	}
	if review != "" {
		return true, s.recordPackagePaymentReview(ctx, tx, o, review)
	}
	if err = s.applyPackageCheckoutGrantTx(ctx, tx, checkout, sub, o, plan); err != nil {
		return true, err
	}
	_, err = tx.Exec(ctx, `UPDATE v3_commerce.package_checkouts SET state='applied' WHERE order_id=$1`, o.ID)
	if err != nil {
		return true, err
	}
	_, err = tx.Exec(ctx, `UPDATE v3_commerce.orders SET fulfillment_state='completed' WHERE id=$1`, o.ID)
	return true, err
}

type packageCheckoutIntent struct {
	subscriptionID, account  int64
	state                    string
	quotedUsed, quotedRemain credits.Micro
	preserveRemaining        bool
	bonus                    credits.Micro
}

func loadPackageCheckoutForApplyTx(ctx context.Context, tx pgx.Tx, orderID int64) (packageCheckoutIntent, bool, error) {
	var c packageCheckoutIntent
	err := tx.QueryRow(ctx, `SELECT target_subscription_id,source_account_id,state,quoted_used,quoted_remaining,preserve_remaining,bonus_credits
	 FROM v3_commerce.package_checkouts WHERE order_id=$1 FOR UPDATE`, orderID).Scan(&c.subscriptionID, &c.account, &c.state, &c.quotedUsed, &c.quotedRemain, &c.preserveRemaining, &c.bonus)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, false, nil
	}
	return c, err == nil, err
}

type packageCheckoutSubscription struct {
	id, account, user int64
	total, baseline   credits.Micro
}

// verifyPackageCheckoutSubscriptionTx locks the target subscription and
// confirms it still matches the account and user quoted at checkout time.
func (s *Service) verifyPackageCheckoutSubscriptionTx(ctx context.Context, tx pgx.Tx, c packageCheckoutIntent, o Order) (packageCheckoutSubscription, bool, error) {
	sub := packageCheckoutSubscription{id: c.subscriptionID}
	var actual int64
	var subState string
	err := tx.QueryRow(ctx, `SELECT account_id,user_id,total_credits,used_credits,state FROM v3_commerce.subscriptions WHERE id=$1 FOR UPDATE`, c.subscriptionID).Scan(&actual, &sub.user, &sub.total, &sub.baseline, &subState)
	if err != nil {
		return sub, false, err
	}
	sub.account = actual
	if actual != c.account || sub.user != o.UserID || subState != "active" {
		return sub, false, nil
	}
	return sub, true, nil
}

// packageCheckoutPlan holds the computed credit grant for the new period
// along with the total/period figures the subscription row is rewritten to.
type packageCheckoutPlan struct {
	newTotal, period, grant credits.Micro
}

// quotePackageCheckoutGrantTx verifies spending has not drifted since the
// quote was taken and computes the credit grant for the new period. A
// non-empty review reason means the caller must flag the payment for review
// instead of applying it.
func (s *Service) quotePackageCheckoutGrantTx(ctx context.Context, tx pgx.Tx, c packageCheckoutIntent, sub packageCheckoutSubscription, o Order) (packageCheckoutPlan, string, error) {
	var plan packageCheckoutPlan
	if s.cfg.FundingDrain != nil {
		drained, err := s.cfg.FundingDrain.FreezeAndDrained(ctx, tx, sub.account)
		if err != nil {
			return plan, "", err
		}
		if !drained {
			return plan, "", ErrFundingPending
		}
	}
	var spent credits.Micro
	if err := tx.QueryRow(ctx, `SELECT COALESCE(GREATEST(-SUM(amount),0),0)::bigint FROM v3_billing.ledger_entries WHERE account_id=$1 AND kind IN ('usage','refund')`, sub.account).Scan(&spent); err != nil {
		return plan, "", err
	}
	actualUsed, err := sub.baseline.Add(spent)
	if err != nil {
		return plan, "", err
	}
	quotedTotal, err := c.quotedUsed.Add(c.quotedRemain)
	if err != nil {
		return plan, "", err
	}
	if actualUsed != c.quotedUsed || (sub.total > 0 && sub.total < quotedTotal) {
		return plan, "quoted spending changed before payment confirmation", nil
	}
	extra := credits.Micro(0)
	if sub.total > 0 {
		extra = sub.total - quotedTotal
	}
	newTotal := o.Credits
	if newTotal > 0 {
		if newTotal, err = newTotal.Add(c.bonus); err != nil {
			return plan, "", err
		}
		if newTotal, err = newTotal.Add(extra); err != nil {
			return plan, "", err
		}
		if c.preserveRemaining {
			if newTotal, err = newTotal.Add(c.quotedRemain); err != nil {
				return plan, "", err
			}
		}
	}
	period := o.PeriodCredits
	if period > 0 {
		if period, err = period.Add(c.bonus); err != nil {
			return plan, "", err
		}
	}
	grant := newTotal
	if newTotal == 0 {
		grant = period
	} else if period > 0 {
		grant = min(grant, period)
	}
	plan.newTotal, plan.period, plan.grant = newTotal, period, grant
	return plan, "", nil
}

// applyPackageCheckoutGrantTx rotates the subscription's credit bucket into
// the new period and rewrites the subscription row to the new plan/order.
func (s *Service) applyPackageCheckoutGrantTx(ctx context.Context, tx pgx.Tx, c packageCheckoutIntent, sub packageCheckoutSubscription, o Order, plan packageCheckoutPlan) error {
	start := s.cfg.Now()
	end := durationEnd(start, o.DurationUnit, o.DurationValue, o.CustomSeconds, o.PeriodSeconds)
	next := nextReset(start, o.ResetPeriod, o.ResetCustomSeconds, end)
	newAccount, err := s.rotateSubscriptionBucket(ctx, tx, sub.id, sub.user, sub.account, plan.grant, "package:"+o.TradeNo, start)
	if err != nil {
		return err
	}
	renewable, err := o.Credits.Add(c.bonus)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE v3_commerce.subscriptions SET plan_id=$2,order_id=$3,account_id=$4,starts_at=$5,expires_at=$6,
	 total_credits=$7,renewable_credits=$8,used_credits=0,period_credits=$9,period_used=0,legacy_periodic=$10,model_usage='{}'::jsonb,
	 model_limits=(SELECT model_limits FROM v3_commerce.plans WHERE id=$2),source='order',
	 last_reset_at=$5,next_reset_at=$11,reset_period=$12,reset_custom_seconds=$13,ended_at=NULL WHERE id=$1`, sub.id, *o.PlanID, o.ID, newAccount, start, end, int64(plan.newTotal), int64(renewable), int64(plan.period), o.LegacyPeriodic, next, o.ResetPeriod, o.ResetCustomSeconds)
	if err != nil {
		return err
	}
	return s.ApplySubscriptionUpgradeGroupTx(ctx, tx, sub.id, *o.PlanID)
}

func (s *Service) RestorePackageCheckout(ctx context.Context, orderID int64) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var orderState, fulfillment string
		if err := tx.QueryRow(ctx, `SELECT state,fulfillment_state FROM v3_commerce.orders WHERE id=$1 FOR UPDATE`, orderID).Scan(&orderState, &fulfillment); err != nil {
			return err
		}
		var id, account int64
		var state string
		err := tx.QueryRow(ctx, `SELECT target_subscription_id,source_account_id,state FROM v3_commerce.package_checkouts WHERE order_id=$1 FOR UPDATE`, orderID).Scan(&id, &account, &state)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if state == "applied" || state == "restored" {
			return nil
		}
		terminal := orderState == "failed" || orderState == "canceled" || orderState == "expired"
		unfulfilledPayment := (orderState == "paid" || orderState == "refunded") && fulfillment == "requires_review"
		if !terminal && !unfulfilledPayment {
			return ErrStateConflict
		}
		if err = s.restorePackageCheckoutSubscriptionTx(ctx, tx, orderID, id, account); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE v3_commerce.package_checkouts SET state='restored' WHERE order_id=$1`, orderID)
		return err
	})
}

// restorePackageCheckoutSubscriptionTx rolls the subscription's bucket back
// to its current (post-drain) balance, since the pending package renewal is
// being abandoned.
func (s *Service) restorePackageCheckoutSubscriptionTx(ctx context.Context, tx pgx.Tx, orderID, id, account int64) error {
	var user, actual int64
	var used, periodUsed, spent, balance credits.Micro
	var expires time.Time
	var subState string
	if err := tx.QueryRow(ctx, `SELECT user_id,account_id,used_credits,period_used,expires_at,state FROM v3_commerce.subscriptions WHERE id=$1 FOR UPDATE`, id).Scan(&user, &actual, &used, &periodUsed, &expires, &subState); err != nil {
		return err
	}
	if actual != account {
		return ErrStateConflict
	}
	if s.cfg.FundingDrain != nil {
		drained, err := s.cfg.FundingDrain.FreezeAndDrained(ctx, tx, account)
		if err != nil {
			return err
		}
		if !drained {
			return ErrFundingPending
		}
	}
	if err := tx.QueryRow(ctx, `SELECT COALESCE(GREATEST(-SUM(amount),0),0)::bigint FROM v3_billing.ledger_entries WHERE account_id=$1 AND kind IN ('usage','refund')`, account).Scan(&spent); err != nil {
		return err
	}
	var err error
	used, err = used.Add(spent)
	if err != nil {
		return err
	}
	periodUsed, err = periodUsed.Add(spent)
	if err != nil {
		return err
	}
	if err = tx.QueryRow(ctx, `SELECT balance FROM v3_billing.accounts WHERE id=$1 FOR UPDATE`, account).Scan(&balance); err != nil {
		return err
	}
	grant := max(balance, 0)
	if !expires.After(s.cfg.Now()) || subState != "active" {
		grant = 0
	}
	newAccount, err := s.rotateSubscriptionBucket(ctx, tx, id, user, account, grant, "restore:"+strconv.FormatInt(orderID, 10), s.cfg.Now())
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE v3_commerce.subscriptions SET account_id=$2,used_credits=$3,period_used=$4 WHERE id=$1`, id, newAccount, int64(used), int64(periodUsed))
	return err
}

func (s *Service) RecoverPackageCheckouts(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `SELECT pc.order_id FROM v3_commerce.package_checkouts pc JOIN v3_commerce.orders o ON o.id=pc.order_id
	 WHERE pc.state IN ('preparing','checkout') AND (o.state IN ('canceled','failed','expired') OR (o.state IN ('paid','refunded') AND o.fulfillment_state='requires_review') OR o.expires_at<=$1 OR o.payment_url='') ORDER BY pc.created_at LIMIT $2`, s.cfg.Now(), limit)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return 0, err
	}
	count := 0
	for _, id := range ids {
		if _, err = s.pool.Exec(ctx, `UPDATE v3_commerce.orders SET state='expired' WHERE id=$1 AND state='created' AND expires_at<=$2`, id, s.cfg.Now()); err != nil {
			return count, err
		}
		var state string
		if err = s.pool.QueryRow(ctx, `SELECT state FROM v3_commerce.orders WHERE id=$1`, id).Scan(&state); err != nil {
			return count, err
		}
		if state == "created" {
			_, err = s.resumePackageCheckout(ctx, id)
		} else {
			err = s.RestorePackageCheckout(ctx, id)
		}
		if errors.Is(err, ErrFundingPending) {
			continue
		}
		if errors.Is(err, ErrStateConflict) {
			var restored bool
			if readErr := s.pool.QueryRow(ctx, `SELECT state='restored' FROM v3_commerce.package_checkouts WHERE order_id=$1`, id).Scan(&restored); readErr != nil {
				return count, readErr
			}
			if restored {
				count++
				continue
			}
		}
		if err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}
