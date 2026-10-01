package commerce

import (
	"context"
	"errors"
	"math/big"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// ConfirmRefundTotal records an externally confirmed cumulative refunded
// amount. Partial topup grants are reversed proportionally with integer half-up
// rounding; the full amount reverses exactly the original grant. This is not a
// browser request to send a refund: only verified payment callbacks call it.
func (s *Service) ConfirmRefundTotal(ctx context.Context, provider, tradeNo, eventID, currency string, amount int64) error {
	if eventID == "" || amount <= 0 || s.poster == nil {
		return ErrInvalid
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		o, err := scanOrder(tx.QueryRow(ctx, `SELECT `+orderColumns+` FROM v3_commerce.orders WHERE trade_no=$1 FOR UPDATE`, tradeNo))
		if err != nil {
			return err
		}
		if o.Provider != provider || currency != o.Currency || amount > o.AmountMinor {
			return ErrPaymentMismatch
		}
		if o.State != "paid" && o.State != "refunded" {
			return ErrStateConflict
		}
		// A subscription refund additionally affects remaining period benefits.
		// Its lifecycle port currently supports a full cancellation only.
		if (o.Kind == "subscription" || o.Kind == "blind_box") && amount != o.AmountMinor {
			return ErrPaymentMismatch
		}
		if err = claimPaymentEvent(ctx, tx, provider, PaymentEvent{ID: eventID, TradeNo: tradeNo, AmountMinor: amount, Currency: currency, Refunded: true}); err != nil {
			return err
		}
		var previous, reversed int64
		err = tx.QueryRow(ctx, `SELECT amount_minor,reversed_credits FROM v3_commerce.provider_refund_progress WHERE order_id=$1`, o.ID).Scan(&previous, &reversed)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if amount <= previous || o.State == "refunded" {
			return nil
		}
		total, err := s.applyRefundProgressTx(ctx, tx, o, eventID, tradeNo, amount, reversed)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO v3_commerce.provider_refund_progress(order_id,amount_minor,reversed_credits,updated_at)
		    VALUES($1,$2,$3,$4) ON CONFLICT(order_id) DO UPDATE SET amount_minor=EXCLUDED.amount_minor,
		    reversed_credits=EXCLUDED.reversed_credits,updated_at=EXCLUDED.updated_at`, o.ID, amount, int64(total), s.cfg.Now())
		if err != nil {
			return err
		}
		if amount == o.AmountMinor {
			tag, err := tx.Exec(ctx, `UPDATE v3_commerce.orders SET state='refunded',refunded_at=$2 WHERE id=$1 AND state='paid'`, o.ID, s.cfg.Now())
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return ErrStateConflict
			}
		}
		return nil
	})
}

// applyRefundProgressTx reverses the proportional credit grant for this
// cumulative refund amount, routing to the kind-specific reversal (full
// subscription cancellation, cash box refund, or a plain wallet adjustment)
// unless the order is held for discount review, in which case nothing is
// reversed yet. Returns the newly computed cumulative reversed-credit total.
func (s *Service) applyRefundProgressTx(ctx context.Context, tx pgx.Tx, o Order, eventID, tradeNo string, amount, reversed int64) (credits.Micro, error) {
	total := refundCredits(o.Credits, amount, o.AmountMinor)
	review, err := s.CheckoutDiscountReviewTx(ctx, tx, o.ID)
	if err != nil {
		return 0, err
	}
	switch {
	case review:
		return 0, nil
	case o.Kind == "subscription":
		return total, s.cancelSubscriptionOrder(ctx, tx, o.ID)
	case o.Kind == "blind_box":
		return total, s.RefundCashBoxTx(ctx, tx, o, amount)
	default:
		delta := total - credits.Micro(reversed)
		if delta <= 0 {
			return total, nil
		}
		account, err := walletTx(ctx, tx, o.UserID)
		if err != nil {
			return 0, err
		}
		_, err = s.poster.PostTx(ctx, tx, billing.Entry{AccountID: account, Amount: -delta, Kind: "adjustment",
			OperationID: "order:refund:" + tradeNo + ":" + strconv.FormatInt(amount, 10), Reason: "provider confirmed cumulative refund",
			Metadata: map[string]any{"refund_id": eventID, "order_id": o.ID, "refunded_minor": amount}})
		return total, err
	}
}

func refundCredits(grant credits.Micro, refunded, paid int64) credits.Micro {
	amount := new(big.Int).Mul(big.NewInt(int64(grant)), big.NewInt(refunded))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(amount, big.NewInt(paid), remainder)
	if remainder.Lsh(remainder, 1).Cmp(big.NewInt(paid)) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	return credits.Micro(quotient.Int64())
}
