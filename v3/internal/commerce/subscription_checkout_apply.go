package commerce

import (
	"context"
	"errors"

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
	if o.PolicyVersion == PolicyStandardV2 {
		return sub, false, nil
	}
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
	 last_reset_at=$5,next_reset_at=$11,reset_period=$12,reset_custom_seconds=$13,ended_at=NULL,policy_version=$14,plan_snapshot=$15,recognized_revenue_credits=$16 WHERE id=$1`, sub.id, *o.PlanID, o.ID, newAccount, start, end, int64(plan.newTotal), int64(renewable), int64(plan.period), o.LegacyPeriodic, next, o.ResetPeriod, o.ResetCustomSeconds, policyVersion(o.PolicyVersion), o.PlanSnapshot, o.RecognizedRevenueCredits)
	if err != nil {
		return err
	}
	if o.PlanSnapshot.ID > 0 {
		if _, err = tx.Exec(ctx, `UPDATE v3_commerce.subscriptions SET model_limits=COALESCE($2::jsonb,'{}'::jsonb) WHERE id=$1`, sub.id, o.PlanSnapshot.ModelLimits); err != nil {
			return err
		}
	}
	if o.PlanSnapshot.ID > 0 {
		err = applySubscriptionGroupTx(ctx, tx, sub.id, o.PlanSnapshot.UpgradeGroup, o.PlanID, s.cfg.Now())
	} else {
		err = s.ApplySubscriptionUpgradeGroupTx(ctx, tx, sub.id, *o.PlanID)
	}
	if err != nil {
		return err
	}
	return s.applySubscriptionGrantedTx(ctx, tx, sub.user)
}
