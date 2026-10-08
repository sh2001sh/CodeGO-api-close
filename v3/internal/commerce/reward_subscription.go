package commerce

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/marketplace"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// MonthlyBenefits fulfills retained checkout snapshots in the same transaction
// as payment. New purchases and subscription rewards grant no multiplier cards.
type MonthlyBenefits interface {
	GrantMonthlyCardTx(context.Context, pgx.Tx, int64, int64, string) error
}

func (s *Service) SetMonthlyBenefits(benefits MonthlyBenefits) { s.cfg.MonthlyBenefits = benefits }

func (s *Service) grantRewardSubscriptionTx(ctx context.Context, tx pgx.Tx, userID, planID int64, operationID string) error {
	if tx == nil || userID <= 0 || planID <= 0 || operationID == "" || len(operationID) > 200 || s.poster == nil {
		return ErrInvalid
	}
	var locked int64
	if err := tx.QueryRow(ctx, `SELECT id FROM v3_identity.users WHERE id=$1 FOR UPDATE`, userID).Scan(&locked); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "subscription:reward:"+operationID); err != nil {
		return err
	}
	var id int64
	done, err := checkRewardReceiptTx(ctx, tx, operationID, userID, planID)
	if done || err != nil {
		return err
	}
	p, err := scanPlan(tx.QueryRow(ctx, `SELECT `+planColumns+` FROM v3_commerce.plans WHERE id=$1 AND enabled FOR SHARE`, planID))
	if errors.Is(err, pgx.ErrNoRows) {
		return marketplace.ErrNotFound
	}
	if err != nil {
		return err
	}
	if p.PolicyVersion == PolicyStandardV2 {
		id = 0
	} else {
		id, err = s.primaryRewardSubscriptionTx(ctx, tx, userID)
	}
	if err != nil {
		return err
	}
	if id == 0 {
		err = s.grantSubscription(ctx, tx, Order{PolicyVersion: p.PolicyVersion, PlanSnapshot: p, UserID: userID, PlanID: &p.ID, Credits: p.Credits, PeriodCredits: p.PeriodCredits,
			PeriodSeconds: p.PeriodSeconds, ResetPeriod: p.ResetPeriod, ResetCustomSeconds: p.ResetCustomSeconds, TradeNo: operationID,
			DurationUnit: p.DurationUnit, DurationValue: p.DurationValue, CustomSeconds: p.CustomSeconds,
			LegacyPeriodic: p.PeriodCredits == 0 && p.ResetPeriod != "never"})
		if err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `SELECT id FROM v3_commerce.subscriptions WHERE reward_operation=$1`, operationID).Scan(&id)
	} else {
		err = s.mergeRewardSubscriptionTx(ctx, tx, id, userID, p.Credits, operationID)
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO v3_commerce.subscription_reward_receipts(operation_id,user_id,plan_id,subscription_id,credits,monthly_seconds)
	 VALUES($1,$2,$3,$4,$5,0)`, operationID, userID, planID, id, int64(p.Credits))
	return err
}

// checkRewardReceiptTx returns true (done, nil) if this reward operation was
// already granted, requiring the user/plan to match exactly. Older native
// rewards recorded the subscription itself as their receipt, so both tables
// are checked.
func checkRewardReceiptTx(ctx context.Context, tx pgx.Tx, operationID string, userID, planID int64) (bool, error) {
	var priorUser, priorPlan int64
	err := tx.QueryRow(ctx, `SELECT user_id,plan_id FROM v3_commerce.subscription_reward_receipts WHERE operation_id=$1`, operationID).Scan(&priorUser, &priorPlan)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `SELECT user_id,plan_id FROM v3_commerce.subscriptions WHERE reward_operation=$1`, operationID).Scan(&priorUser, &priorPlan)
	}
	if err == nil {
		if priorUser != userID || priorPlan != planID {
			return true, billing.ErrPostConflict
		}
		return true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return true, err
}

func (s *Service) primaryRewardSubscriptionTx(ctx context.Context, tx pgx.Tx, user int64) (int64, error) {
	var id int64
	// The source chooses the highest priced managed package, then its capacity
	// and expiry. One- and two-day passes do not receive monthly rewards.
	err := tx.QueryRow(ctx, `SELECT sub.id FROM v3_commerce.subscriptions sub JOIN v3_commerce.plans p ON p.id=sub.plan_id
	 WHERE sub.user_id=$1 AND sub.state='active' AND sub.deleted_at IS NULL AND sub.starts_at<=$2 AND sub.expires_at>$2
	 AND sub.policy_version='legacy' AND NOT(p.duration_unit='day' AND p.duration_value BETWEEN 1 AND 2)
	 ORDER BY p.price_minor DESC,p.credits DESC,p.period_credits DESC,p.id DESC,sub.expires_at DESC,sub.id DESC
	 LIMIT 1 FOR UPDATE OF sub`, user, s.cfg.Now()).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

func (s *Service) mergeRewardSubscriptionTx(ctx context.Context, tx pgx.Tx, id, user int64, amount credits.Micro, operation string) error {
	if err := s.checkPackagePending(ctx, tx, id); err != nil {
		return err
	}
	var pending bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_commerce.subscription_operations WHERE subscription_id=$1 AND state='pending')`, id).Scan(&pending); err != nil {
		return err
	}
	if pending {
		return ErrFundingPending
	}
	var account int64
	var total, period credits.Micro
	if err := tx.QueryRow(ctx, `SELECT account_id,total_credits,period_credits FROM v3_commerce.subscriptions WHERE id=$1`, id).Scan(&account, &total, &period); err != nil {
		return err
	}
	var err error
	if total > 0 {
		total, err = total.Add(amount)
		if err != nil {
			return err
		}
	}
	if period > 0 {
		period, err = period.Add(amount)
		if err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE v3_commerce.subscriptions SET total_credits=$2,period_credits=$3 WHERE id=$1`, id, int64(total), int64(period)); err != nil {
		return err
	}
	if amount == 0 {
		return nil
	}
	_, err = s.poster.PostTx(ctx, tx, billing.Entry{AccountID: account, Amount: amount, Kind: "subscription_grant",
		OperationID: "subscription:reward:" + operation, Reason: "blind-box subscription reward merged into active package",
		Metadata: map[string]any{"user_id": user, "subscription_id": id, "reward_operation": operation}})
	return err
}

func rewardBenefitReference(order Order) string {
	return fmt.Sprintf("monthly-pass-order:%d", order.ID)
}
