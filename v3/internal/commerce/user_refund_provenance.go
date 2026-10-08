package commerce

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// Native lot balances are authoritative after targeted revocations and peer
// transfers. Ledger FIFO replay remains only for audited imported origins.
func nativeRefundLots(ctx context.Context, q refundQuerier, account int64, out map[string]credits.Micro) error {
	rows, err := q.Query(ctx, `SELECT e.operation_id,coalesce(sum(l.remaining_amount),0)::bigint
	 FROM v3_billing.ledger_entries e LEFT JOIN v3_billing.funding_lots l
	 ON l.account_id=e.account_id AND l.source='topup' AND l.reference_type='topup' AND l.reference_id=e.operation_id
	 WHERE e.account_id=$1 AND e.kind='topup' AND e.amount>0 AND e.operation_id LIKE 'order:paid:%'
	 GROUP BY e.id,e.operation_id ORDER BY e.id`, account)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var operation string
		var remaining credits.Micro
		if err := rows.Scan(&operation, &remaining); err != nil {
			return err
		}
		out[strings.TrimPrefix(operation, "order:paid:")] = remaining
	}
	return rows.Err()
}

// A generic refund debit must actually reserve this order's principal.
// If an older unrelated lot was selected, roll the entire transaction back
// before making any provider request; never relabel conversion proceeds.
func verifyUserRefundReservationTx(ctx context.Context, tx pgx.Tx, p insertUserRefundParams) error {
	if p.order.Kind != "topup" || p.reserved <= 0 {
		return nil
	}
	var total, backed credits.Micro
	err := tx.QueryRow(ctx, `SELECT coalesce(sum(a.amount),0)::bigint,
	 coalesce(sum(a.amount) FILTER(WHERE
	 (l.source='topup' AND l.reference_type='topup' AND l.reference_id='order:paid:'||$3)
	 OR (l.source='topup' AND l.reference_type='topup' AND l.reference_id=$3
	 AND EXISTS(SELECT 1 FROM v3_commerce.user_refund_origins f
	 WHERE f.account_id=$1 AND f.order_id=$4 AND f.remaining_credits >= $5))
	 OR (l.source='legacy_unattributed' AND EXISTS(SELECT 1 FROM v3_commerce.user_refund_origins f
	 WHERE f.account_id=$1 AND f.order_id=$4 AND f.remaining_credits >= $5))),0)::bigint
	 FROM v3_billing.funding_allocations a JOIN v3_billing.funding_lots l ON l.lot_id=a.lot_id
	 WHERE a.account_id=$1 AND a.request_id=$2`, p.account, "native:operation:"+refundOperation(p.no)+":reserve",
		p.order.TradeNo, p.order.ID, int64(p.reserved)).Scan(&total, &backed)
	if err != nil {
		return err
	}
	if total != p.reserved || backed != p.reserved {
		return ErrRefundUnavailable
	}
	return nil
}
