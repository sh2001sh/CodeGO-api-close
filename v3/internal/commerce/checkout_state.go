package commerce

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

func (s *Service) releaseCheckoutTx(ctx context.Context, tx pgx.Tx, o Order) error {
	if err := s.ReleaseCheckoutDiscountTx(ctx, tx, o); err != nil {
		return err
	}
	if s.cfg.OrderReleased != nil {
		if err := s.cfg.OrderReleased(ctx, tx, o); err != nil {
			return err
		}
	}
	if o.Kind == "blind_box" {
		return expireCashBoxTx(ctx, tx, o.TradeNo)
	}
	return nil
}

func (s *Service) failCheckout(ctx context.Context, o Order) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		o, err = scanOrder(tx.QueryRow(ctx, `SELECT `+orderColumns+` FROM v3_commerce.orders WHERE id=$1 FOR UPDATE`, o.ID))
		if err != nil || o.State != "created" {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE v3_commerce.orders SET state='failed' WHERE id=$1`, o.ID); err != nil {
			return err
		}
		o.State = "failed"
		return s.releaseCheckoutTx(ctx, tx, o)
	})
}

func (s *Service) Cancel(ctx context.Context, userID int64, tradeNo string) error {
	var orderID int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		o, err := scanOrder(tx.QueryRow(ctx, `SELECT `+orderColumns+` FROM v3_commerce.orders WHERE user_id=$1 AND trade_no=$2 FOR UPDATE`, userID, tradeNo))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrStateConflict
		}
		if err != nil {
			return err
		}
		if o.State != "created" {
			return ErrStateConflict
		}
		if _, err = tx.Exec(ctx, `UPDATE v3_commerce.orders SET state='canceled' WHERE id=$1`, o.ID); err != nil {
			return err
		}
		o.State, orderID = "canceled", o.ID
		return s.releaseCheckoutTx(ctx, tx, o)
	})
	if err != nil {
		return err
	}
	return s.RestorePackageCheckout(ctx, orderID)
}

func (s *Service) ExpireOrders(ctx context.Context) (int64, error) {
	var count int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+orderColumns+` FROM v3_commerce.orders WHERE state='created' AND expires_at<=$1 ORDER BY id LIMIT 1000 FOR UPDATE SKIP LOCKED`, s.cfg.Now())
		if err != nil {
			return err
		}
		orders, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Order, error) { return scanOrder(row) })
		if err != nil {
			return err
		}
		for _, o := range orders {
			if _, err = tx.Exec(ctx, `UPDATE v3_commerce.orders SET state='expired' WHERE id=$1`, o.ID); err != nil {
				return err
			}
			o.State = "expired"
			if err = s.releaseCheckoutTx(ctx, tx, o); err != nil {
				return err
			}
			count++
		}
		return nil
	})
	return count, err
}
