package commerce

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// SetPaidPurchaseHook attaches the restored referral reward to the same
// transaction that actually delivers a paid package.
func (s *Service) SetPaidPurchaseHook(hook func(context.Context, pgx.Tx, Order) error) {
	s.cfg.PaidPurchase = hook
}

// SetSubscriptionGrantedHook assigns retained monthly benefits for paid,
// administrative and redeemed subscriptions within their grant transaction.
func (s *Service) SetSubscriptionGrantedHook(hook func(context.Context, pgx.Tx, int64) error) {
	s.cfg.SubscriptionGranted = hook
}

func (s *Service) applySubscriptionGrantedTx(ctx context.Context, tx pgx.Tx, user int64) error {
	if s.cfg.SubscriptionGranted == nil {
		return nil
	}
	return s.cfg.SubscriptionGranted(ctx, tx, user)
}

func (s *Service) applyPaidPurchaseTx(ctx context.Context, tx pgx.Tx, order Order) error {
	if s.cfg.PaidPurchase == nil || order.AmountMinor <= 0 || order.PurchaseType == "fuel" {
		return nil
	}
	var review bool
	if err := tx.QueryRow(ctx, `SELECT fulfillment_state='requires_review' FROM v3_commerce.orders WHERE id=$1`, order.ID).Scan(&review); err != nil {
		return err
	}
	if review {
		return nil
	}
	return s.cfg.PaidPurchase(ctx, tx, order)
}

// ResetRewardSubscriptionTx keeps opportunity spending and the real funding
// bucket rotation atomic. The caller must validate the owner's opportunity.
func (s *Service) ResetRewardSubscriptionTx(ctx context.Context, tx pgx.Tx, id int64, operation string) error {
	if id <= 0 || !validOperation(operation) {
		return ErrInvalid
	}
	return s.resetSubscriptionTx(ctx, tx, id, true, operation)
}
