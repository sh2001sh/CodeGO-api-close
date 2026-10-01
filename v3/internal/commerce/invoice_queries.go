package commerce

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Service) ListInvoiceEligibleOrders(ctx context.Context, userID int64) ([]InvoiceEligibleOrder, error) {
	if userID <= 0 {
		return nil, ErrInvalid
	}
	rows, err := s.pool.Query(ctx, `SELECT o.kind,o.trade_no,o.amount_minor,o.currency,o.paid_at,
		CASE WHEN o.kind='topup' THEN '钱包充值' ELSE COALESCE(p.name,'套餐订单') END,
		EXISTS(SELECT 1 FROM v3_commerce.invoice_items i WHERE i.trade_no=o.trade_no)
		OR EXISTS(SELECT 1 FROM v3_commerce.invoices i WHERE i.source_type<>'batch' AND i.trade_no=o.trade_no)
		FROM v3_commerce.orders o LEFT JOIN v3_commerce.plans p ON p.id=o.plan_id
		WHERE o.user_id=$1 AND o.state='paid' AND o.paid_at IS NOT NULL
		AND NOT EXISTS(SELECT 1 FROM v3_commerce.provider_refund_progress r WHERE r.order_id=o.id)
		AND NOT EXISTS(SELECT 1 FROM v3_commerce.user_refunds r WHERE r.order_id=o.id AND r.status<>'failed')
		ORDER BY o.paid_at DESC,o.id DESC`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (InvoiceEligibleOrder, error) {
		var o InvoiceEligibleOrder
		var paid time.Time
		err := row.Scan(&o.SourceType, &o.TradeNo, &o.OrderAmountMinor, &o.Currency, &paid, &o.OrderTitle, &o.Requested)
		o.OrderAmount, o.PaidAt = invoiceAmount(o.OrderAmountMinor, o.Currency), paid.Unix()
		return o, err
	})
}

// A zero userID selects the administration view. HTTP callers must pass the
// authenticated user's ID unless the administrator guard has succeeded.
func (s *Service) ListInvoiceRequests(ctx context.Context, userID int64, status string, page, pageSize int) (InvoicePage, error) {
	if userID < 0 || status != "" && status != "pending" && status != "issued" && status != "rejected" || page < 1 || page > 1_000_000 || pageSize < 1 || pageSize > 100 {
		return InvoicePage{}, ErrInvalid
	}
	result := InvoicePage{Page: page, PageSize: pageSize, Items: []InvoiceRequest{}}
	// Keep total and items on one snapshot even when requests arrive concurrently.
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	const filter = ` FROM v3_commerce.invoices WHERE ($1::bigint=0 OR user_id=$1) AND ($2::text='' OR status=$2)`
	if err = tx.QueryRow(ctx, `SELECT count(*)`+filter, userID, status).Scan(&result.Total); err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, `SELECT `+invoiceColumns+filter+` ORDER BY id DESC LIMIT $3 OFFSET $4`, userID, status, pageSize, (page-1)*pageSize)
	if err != nil {
		return result, err
	}
	result.Items, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (InvoiceRequest, error) { return scanInvoice(row) })
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
