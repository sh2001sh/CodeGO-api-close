package commerce

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

// CompleteCashBoxTx is called after the order and signed receipt have been
// verified. Failure rolls back the receipt, paid state and all inventory.
func (s *Service) CompleteCashBoxTx(ctx context.Context, tx pgx.Tx, o Order) error {
	if o.Kind != "blind_box" {
		return ErrInvalid
	}
	if s.cfg.CashBoxes == nil {
		return ErrProviderUnavailable
	}
	_, err := s.cfg.CashBoxes.CompleteBoxOrderTx(ctx, tx, o.UserID, o.TradeNo, o.AmountMinor, o.Currency)
	return cashBoxError(err)
}

// RefundCashBoxTx rejects partial refunds and orders whose inventory has been
// opened or transferred. Cancellation and refund history share one commit.
func (s *Service) RefundCashBoxTx(ctx context.Context, tx pgx.Tx, o Order, amount int64) error {
	if o.Kind != "blind_box" || amount != o.AmountMinor {
		return ErrPaymentMismatch
	}
	if s.cfg.CashBoxes == nil {
		return ErrProviderUnavailable
	}
	return cashBoxError(s.cfg.CashBoxes.CancelBoxOrderTx(ctx, tx, o.UserID, o.TradeNo))
}

func expireCashBoxTx(ctx context.Context, tx pgx.Tx, trade string) error {
	// A checkout deadline is not an inventory expiration or refund. A later
	// verified paid event must still be able to materialize the purchased boxes.
	_, err := tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_orders SET status='expired' WHERE trade_no=$1 AND status='pending'`, trade)
	return err
}

func (s *Service) failCashBox(ctx context.Context, o Order) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE v3_commerce.orders SET state='failed' WHERE id=$1 AND state='created'`, o.ID)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		return expireCashBoxTx(ctx, tx, o.TradeNo)
	})
}

func (s *Service) CancelCashBox(ctx context.Context, userID int64, trade string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		o, err := scanOrder(tx.QueryRow(ctx, `SELECT `+orderColumns+` FROM v3_commerce.orders WHERE user_id=$1 AND trade_no=$2 AND kind='blind_box' FOR UPDATE`, userID, trade))
		if err != nil {
			return err
		}
		if o.State == "canceled" {
			return expireCashBoxTx(ctx, tx, trade)
		}
		if o.State != "created" {
			return ErrStateConflict
		}
		if _, err = tx.Exec(ctx, `UPDATE v3_commerce.orders SET state='canceled' WHERE id=$1`, o.ID); err != nil {
			return err
		}
		return expireCashBoxTx(ctx, tx, trade)
	})
}

// RecoverCashBoxOrders releases purchase-limit capacity after a generic order
// cancellation, worker expiry or signed failure callback, including after a
// process crash. Already-paid or refunded inventory is never changed.
func (s *Service) RecoverCashBoxOrders(ctx context.Context, limit int) (int64, error) {
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	var count int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT o.trade_no FROM v3_commerce.orders o
		 JOIN v3_marketplace.blind_box_orders b ON b.trade_no=o.trade_no
		 WHERE o.kind='blind_box' AND o.state IN('canceled','expired','failed') AND b.status='pending'
		 ORDER BY o.id LIMIT $1 FOR UPDATE OF o SKIP LOCKED`, limit)
		if err != nil {
			return err
		}
		trades, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		for _, trade := range trades {
			if err = expireCashBoxTx(ctx, tx, trade); err != nil {
				return err
			}
			count++
		}
		return nil
	})
	return count, err
}

type CashBoxStatus struct {
	TradeNo         string      `json:"trade_no"`
	Status          string      `json:"status"`
	Quantity        int         `json:"quantity"`
	OpenedCount     int         `json:"opened_count"`
	Money           json.Number `json:"money"`
	PaymentMethod   string      `json:"payment_method"`
	PaymentProvider string      `json:"payment_provider"`
	CreateTime      int64       `json:"create_time"`
	CompleteTime    int64       `json:"complete_time"`
}

func (s *Service) CashBoxOrder(ctx context.Context, userID int64, trade string) (CashBoxStatus, error) {
	var result CashBoxStatus
	var amount int64
	var currency string
	err := s.pool.QueryRow(ctx, `SELECT o.trade_no,o.state,b.quantity,b.opened_count,o.amount_minor,o.currency,b.payment_method,o.provider,
	 extract(epoch FROM o.created_at)::bigint,coalesce(extract(epoch FROM b.completed_at)::bigint,0)
	 FROM v3_commerce.orders o JOIN v3_marketplace.blind_box_orders b ON b.trade_no=o.trade_no
	 WHERE o.user_id=$1 AND o.trade_no=$2 AND o.kind='blind_box'`, userID, trade).Scan(&result.TradeNo,
		&result.Status, &result.Quantity, &result.OpenedCount, &amount, &currency, &result.PaymentMethod, &result.PaymentProvider,
		&result.CreateTime, &result.CompleteTime)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrNotFound
	}
	result.Money = currencyNumber(amount, currency)
	switch result.Status {
	case "paid":
		result.Status = "success"
	case "created":
		result.Status = "pending"
	}
	return result, err
}
