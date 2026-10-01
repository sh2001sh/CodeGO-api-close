package commerce

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// CreateInvoiceRequest reserves all order claims atomically. The browser supplies
// only order identities; payment totals are read and frozen under row locks.
func (s *Service) CreateInvoiceRequest(ctx context.Context, userID int64, input CreateInvoiceRequestInput) (InvoiceRequest, error) {
	if userID <= 0 {
		return InvoiceRequest{}, ErrInvalid
	}
	input.Orders = append([]InvoiceOrderInput(nil), input.Orders...)
	if err := validateInvoiceCreate(&input); err != nil {
		return InvoiceRequest{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return InvoiceRequest{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	orders, amount, currency, err := loadInvoiceOrders(ctx, tx, userID, input.Orders)
	if err != nil {
		return InvoiceRequest{}, err
	}
	source, trade, title := orders[0].SourceType, orders[0].TradeNo, orders[0].OrderTitle
	if len(orders) > 1 {
		source, title = "batch", "合并开票"
		trade, err = tradeNumber()
		if err != nil {
			return InvoiceRequest{}, err
		}
		trade = "batch-" + trade
	}
	now := s.cfg.Now().UTC()
	result, err := scanInvoice(tx.QueryRow(ctx, `INSERT INTO v3_commerce.invoices
		(user_id,source_type,trade_no,order_amount_minor,order_amount,currency,order_title,order_count,
		invoice_type,title,tax_number,email,remark,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5::numeric,$6,$7,$8,$9,$10,$11,$12,$13,$14,$14) RETURNING `+invoiceColumns,
		userID, source, trade, amount, invoiceAmount(amount, currency).String(), currency, title, len(orders),
		input.InvoiceType, input.Title, input.TaxNumber, input.Email, input.Remark, now))
	if err != nil {
		return InvoiceRequest{}, invoiceDatabaseError(err)
	}
	for _, o := range orders {
		_, err = tx.Exec(ctx, `INSERT INTO v3_commerce.invoice_items
			(invoice_id,user_id,source_type,trade_no,order_amount_minor,order_amount,currency,order_title,paid_at)
			VALUES($1,$2,$3,$4,$5,$6::numeric,$7,$8,$9)`, result.ID, userID, o.SourceType, o.TradeNo,
			o.OrderAmountMinor, o.OrderAmount.String(), o.Currency, o.OrderTitle, time.Unix(o.PaidAt, 0))
		if err != nil {
			return InvoiceRequest{}, invoiceDatabaseError(err)
		}
	}
	return result, tx.Commit(ctx)
}

const invoiceClaimQuery = `SELECT EXISTS(SELECT 1 FROM v3_commerce.invoice_items WHERE trade_no=$1)
OR EXISTS(SELECT 1 FROM v3_commerce.invoices WHERE source_type<>'batch' AND trade_no=$1)`

// loadInvoiceOrders locks and validates each referenced order (eligible,
// unclaimed, same currency as the rest of the batch) and sums their amounts.
func loadInvoiceOrders(ctx context.Context, tx pgx.Tx, userID int64, refs []InvoiceOrderInput) ([]InvoiceEligibleOrder, int64, string, error) {
	orders := make([]InvoiceEligibleOrder, 0, len(refs))
	var amount int64
	currency := ""
	for _, ref := range refs {
		o, err := loadInvoiceOrder(ctx, tx, userID, ref)
		if err != nil {
			return nil, 0, "", err
		}
		var claimed bool
		if err = tx.QueryRow(ctx, invoiceClaimQuery, ref.TradeNo).Scan(&claimed); err != nil {
			return nil, 0, "", err
		}
		if claimed {
			return nil, 0, "", ErrStateConflict
		}
		if currency != "" && currency != o.Currency || amount > math.MaxInt64-o.OrderAmountMinor {
			return nil, 0, "", ErrInvalid
		}
		currency, amount = o.Currency, amount+o.OrderAmountMinor
		orders = append(orders, o)
	}
	return orders, amount, currency, nil
}

func loadInvoiceOrder(ctx context.Context, tx pgx.Tx, userID int64, ref InvoiceOrderInput) (InvoiceEligibleOrder, error) {
	var o InvoiceEligibleOrder
	var paid time.Time
	err := tx.QueryRow(ctx, `SELECT o.kind,o.trade_no,o.amount_minor,o.currency,o.paid_at,
		CASE WHEN o.kind='topup' THEN '钱包充值' ELSE COALESCE(p.name,'套餐订单') END
		FROM v3_commerce.orders o LEFT JOIN v3_commerce.plans p ON p.id=o.plan_id
		WHERE o.user_id=$1 AND o.trade_no=$2 AND o.kind=$3 AND o.state='paid' AND o.paid_at IS NOT NULL
		FOR UPDATE OF o`, userID, ref.TradeNo, ref.SourceType).
		Scan(&o.SourceType, &o.TradeNo, &o.OrderAmountMinor, &o.Currency, &paid, &o.OrderTitle)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	if err != nil {
		return o, err
	}
	// Read refund progress after the order lock: a concurrent partial refund
	// may have committed while this transaction waited for that same lock.
	var refunded bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_commerce.provider_refund_progress r
		JOIN v3_commerce.orders o ON o.id=r.order_id WHERE o.trade_no=$1)
		OR EXISTS(SELECT 1 FROM v3_commerce.user_refunds r JOIN v3_commerce.orders o ON o.id=r.order_id
		WHERE o.trade_no=$1 AND r.status<>'failed')`, ref.TradeNo).Scan(&refunded)
	if err == nil && refunded {
		err = ErrNotFound
	}
	o.OrderAmount, o.PaidAt = invoiceAmount(o.OrderAmountMinor, o.Currency), paid.Unix()
	return o, err
}

func invoiceDatabaseError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return ErrStateConflict
	}
	return err
}

// UpdateAdminInvoiceRequest records a manual outcome. An identical retry is
// idempotent, while both terminal outcomes reject later mutations.
func (s *Service) UpdateAdminInvoiceRequest(ctx context.Context, id, adminID int64, input UpdateInvoiceRequestInput) (InvoiceRequest, error) {
	if id <= 0 || adminID <= 0 {
		return InvoiceRequest{}, ErrInvalid
	}
	if err := validateInvoiceUpdate(&input); err != nil {
		return InvoiceRequest{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return InvoiceRequest{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := scanInvoice(tx.QueryRow(ctx, `SELECT `+invoiceColumns+` FROM v3_commerce.invoices WHERE id=$1 FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return InvoiceRequest{}, ErrNotFound
	}
	if err != nil {
		return InvoiceRequest{}, err
	}
	if result.Status != "pending" {
		if result.Status == input.Status && result.InvoiceNumber == input.InvoiceNumber && result.AdminNote == input.AdminNote {
			return result, tx.Commit(ctx)
		}
		return InvoiceRequest{}, ErrStateConflict
	}
	if input.Status == "issued" {
		if err = invoiceOrdersStillPaid(ctx, tx, result); err != nil {
			return InvoiceRequest{}, err
		}
	}
	now := s.cfg.Now().UTC()
	var issued *time.Time
	if input.Status == "issued" {
		issued = &now
	}
	result, err = scanInvoice(tx.QueryRow(ctx, `UPDATE v3_commerce.invoices SET status=$2,invoice_number=$3,
		admin_note=$4,handled_by=$5,issued_at=$6,updated_at=$7 WHERE id=$1 RETURNING `+invoiceColumns,
		id, input.Status, input.InvoiceNumber, input.AdminNote, adminID, issued, now))
	if err != nil {
		return InvoiceRequest{}, err
	}
	return result, tx.Commit(ctx)
}

func invoiceOrdersStillPaid(ctx context.Context, tx pgx.Tx, invoice InvoiceRequest) error {
	rows, err := tx.Query(ctx, `SELECT source_type,trade_no FROM v3_commerce.invoice_items WHERE invoice_id=$1 ORDER BY trade_no`, invoice.ID)
	if err != nil {
		return err
	}
	refs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (InvoiceOrderInput, error) {
		var ref InvoiceOrderInput
		err := row.Scan(&ref.SourceType, &ref.TradeNo)
		return ref, err
	})
	if err != nil {
		return err
	}
	// Pre-batch historical requests may have no item rows.
	if len(refs) == 0 && invoice.SourceType != "batch" {
		refs = []InvoiceOrderInput{{SourceType: invoice.SourceType, TradeNo: invoice.TradeNo}}
	}
	if len(refs) != invoice.OrderCount {
		return ErrStateConflict
	}
	for _, ref := range refs {
		if _, err = loadInvoiceOrder(ctx, tx, invoice.UserID, ref); err != nil {
			if errors.Is(err, ErrNotFound) {
				return ErrStateConflict
			}
			return err
		}
	}
	return nil
}
