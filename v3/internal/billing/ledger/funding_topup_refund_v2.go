package ledger

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// Verified order adjustments and durable owner refunds select their own topup.
// Commerce still verifies exact-order funding before dispatching owner refunds.
func fundingTopupRefundTrade(e billing.Entry) string {
	trade, _ := e.Metadata["refund_trade_no"].(string)
	if trade == "" || e.Amount >= 0 {
		return ""
	}
	switch e.Kind {
	case "adjustment":
		operation := "order:refund:" + trade
		if e.OperationID == operation || strings.HasPrefix(e.OperationID, operation+":") {
			return trade
		}
	case "refund":
		refund, _ := e.Metadata["refund_no"].(string)
		if refund != "" && e.OperationID == "user-refund:"+refund+":reserve" {
			return trade
		}
	}
	return ""
}

// Order refunds reclaim their own topup first; the ordinary FIFO query retains
// its existing ordering/index path without a refund-priority sort expression.
func lockFundingLotsTx(ctx context.Context, tx pgx.Tx, account int64, topupTrade string) ([]fundingLot, credits.Micro, error) {
	order := "created_at,lot_id"
	args := []any{account}
	if topupTrade != "" {
		// Imported lots retain the original merchant trade number. Only an
		// audited order/account origin can authorize that historical identity.
		order = `(source='topup' AND reference_type='topup' AND
		 (reference_id='order:paid:'||$2 OR (reference_id=$2 AND EXISTS(
		 SELECT 1 FROM v3_commerce.user_refund_origins f JOIN v3_commerce.orders o ON o.id=f.order_id
		 WHERE f.account_id=$1 AND o.trade_no=$2)))) DESC,created_at,lot_id`
		args = append(args, topupTrade)
	}
	rows, err := tx.Query(ctx, `SELECT lot_id,source_account_id,source,remaining_amount,revenue_multiplier_ppm,non_transferable,non_refundable FROM v3_billing.funding_lots
	 WHERE account_id=$1 AND remaining_amount>0 ORDER BY `+order+` FOR UPDATE`, args...)
	if err != nil {
		return nil, 0, err
	}
	var lots []fundingLot
	var known credits.Micro
	for rows.Next() {
		var lot fundingLot
		if err := rows.Scan(&lot.id, &lot.sourceAccount, &lot.source, &lot.remaining, &lot.ppm, &lot.nonTransferable, &lot.nonRefundable); err != nil {
			rows.Close()
			return nil, 0, err
		}
		known, err = known.Add(credits.Micro(lot.remaining))
		if err != nil {
			rows.Close()
			return nil, 0, err
		}
		lots = append(lots, lot)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return lots, known, nil
}
