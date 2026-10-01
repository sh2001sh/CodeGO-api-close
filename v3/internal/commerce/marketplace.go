package commerce

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/marketplace"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// GroupPurchaseTx validates ownership and paid state inside the marketplace
// transaction. Bonus and group rules are frozen on the original order.
func (s *Service) GroupPurchaseTx(ctx context.Context, tx pgx.Tx, userID, orderID int64) (marketplace.GroupPurchase, error) {
	var result marketplace.GroupPurchase
	var lifetime int64
	err := tx.QueryRow(ctx, `SELECT o.id,o.plan_id,s.id,s.account_id,o.group_buy_enabled,o.group_buy_target,o.group_buy_bonus,o.group_buy_lifetime_seconds,
	    o.group_buy_bonus2_micro,o.group_buy_bonus3_micro,o.group_buy_bonus5_micro
	    FROM v3_commerce.orders o JOIN v3_commerce.subscriptions s ON s.order_id=o.id
	    WHERE o.id=$1 AND o.user_id=$2 AND o.state='paid' AND o.kind='subscription'
	    AND s.state='active' AND s.starts_at<=$3 AND s.expires_at>$3 FOR UPDATE OF o`, orderID, userID, s.cfg.Now()).
		Scan(&result.OrderID, &result.PlanID, &result.SubscriptionID, &result.BonusAccountID, &result.Enabled, &result.TargetCount, &result.BonusMicro, &lifetime,
			&result.BonusAt2Micro, &result.BonusAt3Micro, &result.BonusAt5Micro)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, marketplace.ErrNotFound
	}
	if err != nil {
		return result, err
	}
	result.Lifetime = time.Duration(lifetime) * time.Second
	return result, nil
}

// GrantRewardTx shares the marketplace transaction and merges rewards into the
// primary active package without replacing its tier or expiration.
func (s *Service) GrantRewardTx(ctx context.Context, tx pgx.Tx, userID, planID int64, operationID string) error {
	return s.grantRewardSubscriptionTx(ctx, tx, userID, planID, operationID)
}

// PrepareGroupBonusTx follows the current subscription account after a reset
// and grows its finite budgets in the same transaction as the bonus posting.
func (s *Service) PrepareGroupBonusTx(ctx context.Context, tx pgx.Tx, userID, subscriptionID int64, amount credits.Micro) (int64, error) {
	if userID <= 0 || subscriptionID <= 0 || amount <= 0 || tx == nil {
		return 0, ErrInvalid
	}
	var account int64
	var total, period, renewable credits.Micro
	err := tx.QueryRow(ctx, `SELECT account_id,total_credits,period_credits,renewable_credits FROM v3_commerce.subscriptions
	 WHERE id=$1 AND user_id=$2 AND state='active' AND deleted_at IS NULL AND starts_at<=$3 AND expires_at>$3 FOR UPDATE`, subscriptionID, userID, s.cfg.Now()).Scan(&account, &total, &period, &renewable)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, marketplace.ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if err = s.checkPackagePending(ctx, tx, subscriptionID); err != nil {
		return 0, err
	}
	var pending bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_commerce.subscription_operations WHERE subscription_id=$1 AND state='pending')`, subscriptionID).Scan(&pending); err != nil {
		return 0, err
	}
	if pending {
		return 0, ErrFundingPending
	}
	if total > 0 {
		total, err = total.Add(amount)
		if err != nil {
			return 0, err
		}
	}
	if period > 0 {
		period, err = period.Add(amount)
		if err != nil {
			return 0, err
		}
	}
	renewable, err = renewable.Add(amount)
	if err != nil {
		return 0, err
	}
	_, err = tx.Exec(ctx, `UPDATE v3_commerce.subscriptions SET total_credits=$2,period_credits=$3,renewable_credits=$4 WHERE id=$1`, subscriptionID, int64(total), int64(period), int64(renewable))
	return account, err
}
