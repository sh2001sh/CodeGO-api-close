package commerce

import (
	"context"
	"errors"
	"math"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/marketplace"
)

// CashBoxMarket shares inventory changes with commerce's verified payment
// transaction. No payment callback can credit a wallet for a sealed box order.
type CashBoxMarket interface {
	CreateBoxOrderTx(context.Context, pgx.Tx, marketplace.BoxOrderInput) (marketplace.BoxOrder, error)
	CompleteBoxOrderTx(context.Context, pgx.Tx, int64, string, int64, string) (marketplace.Purchase, error)
	CancelBoxOrderTx(context.Context, pgx.Tx, int64, string) error
}

// SetCashBoxMarket is a startup composition step after marketplace.New, which
// itself uses commerce's subscription grant port. Call before serving requests.
func (s *Service) SetCashBoxMarket(m CashBoxMarket) { s.cfg.CashBoxes = m }

type CreateCashBox struct {
	UserID, PoolID          int64
	Quantity                int
	Provider, PaymentMethod string
	SuccessURL, CancelURL   string
}

type CashBoxQuote struct {
	PoolID      int64 `json:"pool_id"`
	Quantity    int   `json:"quantity"`
	AmountMinor int64 `json:"amount_minor"`
}

func cashBoxQuoteTx(ctx context.Context, tx pgx.Tx, poolID int64, quantity int) (CashBoxQuote, error) {
	q := CashBoxQuote{PoolID: poolID, Quantity: quantity}
	if poolID < 0 || quantity < 1 || quantity > 100 {
		return q, ErrInvalid
	}
	if poolID == 0 {
		var ids []int64
		rows, err := tx.Query(ctx, `SELECT id FROM v3_marketplace.blind_box_pools WHERE scope='standard' AND enabled ORDER BY id LIMIT 2`)
		if err != nil {
			return q, err
		}
		ids, err = pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return q, err
		}
		if len(ids) != 1 {
			return q, ErrProviderUnavailable
		}
		q.PoolID = ids[0]
	}
	var price int64
	err := tx.QueryRow(ctx, `SELECT price_micro FROM v3_marketplace.blind_box_pools WHERE id=$1 AND scope='standard' AND enabled FOR SHARE`, q.PoolID).Scan(&price)
	if errors.Is(err, pgx.ErrNoRows) {
		return q, ErrNotFound
	}
	if err != nil {
		return q, err
	}
	// Standard cash pools preserve v2 UnitPrice in CNY, scaled by 1e6.
	// Wallet pools use credits and are deliberately excluded by the query.
	if price <= 0 || price%10000 != 0 || price/10000 > math.MaxInt64/int64(quantity) {
		return q, ErrInvalid
	}
	q.AmountMinor = price / 10000 * int64(quantity)
	return q, nil
}

func (s *Service) QuoteCashBox(ctx context.Context, userID, poolID int64, quantity int) (CashBoxQuote, error) {
	var q CashBoxQuote
	if userID <= 0 {
		return q, ErrInvalid
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		q, err = cashBoxQuoteTx(ctx, tx, poolID, quantity)
		return err
	})
	return q, err
}

func (s *Service) CreateCashBox(ctx context.Context, in CreateCashBox) (Order, error) {
	var o Order
	if in.UserID <= 0 || (in.Provider != "epay" && in.Provider != "xunhu") ||
		!s.allowedReturn(in.SuccessURL) || !s.allowedReturn(in.CancelURL) {
		return o, ErrInvalid
	}
	p := s.providers[in.Provider]
	if p == nil || s.cfg.CashBoxes == nil {
		return o, ErrProviderUnavailable
	}
	paymentMethod, err := resolveCashBoxPaymentMethod(p, in.Provider, in.PaymentMethod)
	if err != nil {
		return o, err
	}
	in.PaymentMethod = paymentMethod
	trade, err := tradeNumber()
	if err != nil {
		return o, err
	}
	now := s.cfg.Now()
	o = Order{UserID: in.UserID, Kind: "blind_box", Provider: in.Provider, Currency: "cny", TradeNo: trade,
		CreatedAt: now, ExpiresAt: now.Add(s.cfg.OrderTTL), ResetPeriod: "never", GroupBuyTarget: 3, GroupBuyLifetimeSeconds: 86400}
	o.Selection.PaymentMethod = in.PaymentMethod
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q, err := cashBoxQuoteTx(ctx, tx, in.PoolID, in.Quantity)
		if err != nil {
			return err
		}
		o.AmountMinor = q.AmountMinor
		o, err = s.insertOrderTx(ctx, tx, o, 0)
		if err != nil {
			return err
		}
		_, err = s.cfg.CashBoxes.CreateBoxOrderTx(ctx, tx, marketplace.BoxOrderInput{UserID: in.UserID, PoolID: q.PoolID,
			Quantity: in.Quantity, AmountMinor: q.AmountMinor, TradeNo: trade, Currency: "cny", PaymentProvider: in.Provider,
			PaymentMethod: in.PaymentMethod, Source: "purchase"})
		return cashBoxError(err)
	})
	if err != nil {
		return o, err
	}
	checkout, err := s.checkout(ctx, o, in.SuccessURL, in.CancelURL)
	if err == nil && (checkout.Reference == "" || !validCheckoutURL(checkout.URL) ||
		(checkout.QRCodeURL != "" && !validCheckoutURL(checkout.QRCodeURL))) {
		err = ErrProviderUnavailable
	}
	if err != nil {
		return o, errors.Join(err, s.failCashBox(ctx, o))
	}
	_, err = s.pool.Exec(ctx, `UPDATE v3_commerce.orders SET provider_reference=$2,payment_url=$3,checkout_qrcode_url=$4 WHERE id=$1`, o.ID,
		checkout.Reference, checkout.URL, checkout.QRCodeURL)
	if err != nil {
		return o, err
	}
	return s.GetOrder(ctx, in.UserID, trade)
}

// resolveCashBoxPaymentMethod normalizes the requested payment method for
// the chosen provider: xunhu accepts only its own name, epay defaults to the
// provider's configured payment type when none is requested.
func resolveCashBoxPaymentMethod(p PaymentProvider, provider, requested string) (string, error) {
	if provider == "xunhu" {
		if requested != "" && requested != "xunhu" {
			return "", ErrInvalid
		}
		return "xunhu", nil
	}
	if requested != "" {
		if !paymentToken(requested) {
			return "", ErrInvalid
		}
		return requested, nil
	}
	if epay, ok := p.(*Epay); ok {
		return epay.cfg.PaymentType, nil
	}
	return "", nil
}

func cashBoxError(err error) error {
	switch {
	case errors.Is(err, marketplace.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, marketplace.ErrInvalidInput):
		return ErrInvalid
	case errors.Is(err, marketplace.ErrUnavailable):
		return ErrProviderUnavailable
	case errors.Is(err, marketplace.ErrConflict), errors.Is(err, marketplace.ErrDailyLimit), errors.Is(err, marketplace.ErrMonthlyLimit):
		return ErrStateConflict
	default:
		return err
	}
}
