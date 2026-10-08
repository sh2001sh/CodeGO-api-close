package commerce

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type ConvertedFundingRevoker interface {
	RevokeSubscriptionConversionTx(context.Context, pgx.Tx, int64, int64, string) (credits.Micro, credits.Micro, error)
}

func (s *Service) refundConvertedSubscriptionTx(ctx context.Context, tx pgx.Tx, o Order) (bool, error) {
	var id int64
	var mainOrder *int64
	err := tx.QueryRow(ctx, `SELECT s.id,s.order_id FROM v3_commerce.subscriptions s WHERE s.converted_at IS NOT NULL
 AND (s.order_id=$1 OR (s.id=$2 AND EXISTS(SELECT 1 FROM v3_commerce.subscription_wallet_conversions c JOIN v3_commerce.subscription_wallet_quotes q ON q.quote_id=c.quote_id,
 jsonb_array_elements(q.segments) part WHERE c.subscription_id=s.id AND c.state='completed' AND part->>'original_order_id'=$3))) FOR UPDATE OF s`, o.ID, o.TargetSubscriptionID, fmt.Sprint(o.ID)).Scan(&id, &mainOrder)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	revoker, ok := s.poster.(ConvertedFundingRevoker)
	if !ok {
		return true, ErrStateConflict
	}
	_, exposure, err := revoker.RevokeSubscriptionConversionTx(ctx, tx, id, o.ID, fmt.Sprintf("subscription:converted-refund:%d", o.ID))
	if err != nil {
		return true, err
	}
	// The revoker now holds all origin wallet locks. A previously pending
	// owner refund must settle before these lots can be permanently revoked.
	// Rejecting here rolls back the revocation and callback receipt together.
	var pending bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_commerce.user_refunds r
	 JOIN v3_billing.funding_allocations a ON a.account_id=r.account_id
	 AND a.request_id='native:operation:user-refund:'||r.refund_no||':reserve'
	 JOIN v3_billing.funding_lots l ON l.lot_id=a.lot_id AND l.source='subscription_conversion'
	 WHERE r.status='processing' AND l.metadata->>'subscription_id'=$1
	 AND l.metadata->>'original_order_id'=$2)`, fmt.Sprint(id), fmt.Sprint(o.ID)).Scan(&pending)
	if err != nil {
		return true, err
	}
	if pending {
		return true, ErrFundingPending
	}
	if exposure > 0 {
		if err = s.recordPackagePaymentReview(ctx, tx, o, fmt.Sprintf("provider refund after converted entitlement consumption: %d micro credits require reconciliation", exposure)); err != nil {
			return true, err
		}
	}
	// Refunding a supplemental source revokes only that source's converted
	// funds; the original paid membership benefit keeps its original deadline.
	if mainOrder == nil || *mainOrder != o.ID {
		return true, nil
	}
	if _, err = tx.Exec(ctx, `UPDATE v3_commerce.subscriptions SET benefits_until=NULL WHERE id=$1`, id); err != nil {
		return true, err
	}
	return true, RestoreSubscriptionGroupTx(ctx, tx, id, s.cfg.Now())
}
