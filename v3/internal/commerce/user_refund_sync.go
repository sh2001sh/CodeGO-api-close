package commerce

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
)

func (s *UserRefunds) Sync(ctx context.Context, uid int64, no string) (UserRefundResult, error) {
	if uid <= 0 || strings.TrimSpace(no) == "" {
		return UserRefundResult{}, ErrInvalid
	}
	if s.provider == nil || s.poster == nil {
		return UserRefundResult{}, ErrProviderUnavailable
	}
	return s.dispatch(ctx, uid, no, false)
}

func (s *UserRefunds) dispatch(ctx context.Context, uid int64, no string, create bool) (UserRefundResult, error) {
	var result UserRefundResult
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		r, err := scanUserRefund(tx.QueryRow(ctx, `SELECT `+userRefundColumns+` FROM v3_commerce.user_refunds r JOIN v3_commerce.orders o ON o.id=r.order_id
		 WHERE r.refund_no=$1 AND r.user_id=$2 FOR UPDATE OF r`, no, uid))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		result = r.UserRefundResult
		if r.Status != "processing" {
			return nil
		}
		var response RefundProviderResult
		if create {
			response, err = s.provider.CreateRefund(ctx, RefundPayment{OrderID: r.providerOrderID, RefundNo: no, AmountMinor: r.AmountMinor})
		} else {
			response, err = s.provider.QueryRefund(ctx, r.RefundID, no)
		}
		if (!create && err != nil && r.RefundID == "") || (err == nil && response.State == "not_found") {
			// Query first after ambiguity. Without a provider ID, a crash may
			// have happened before dispatch; retry the exact merchant key.
			response, err = s.provider.CreateRefund(ctx, RefundPayment{OrderID: r.providerOrderID, RefundNo: no, AmountMinor: r.AmountMinor})
		}
		if err == nil && (response.RefundNo != no || response.AmountMinor != r.AmountMinor ||
			(response.State != "processing" && response.State != "success" && response.State != "failed")) {
			err = ErrPaymentMismatch
		}
		if err != nil {
			// An HTTP failure is ambiguous: it never releases already withdrawn
			// credit. The same durable refund number is queried on every retry.
			result.Message = "退款结果尚未确认，请稍后同步"
			_, updateErr := tx.Exec(ctx, `UPDATE v3_commerce.user_refunds SET last_error=$2,updated_at=$3 WHERE refund_no=$1`, no, err.Error(), s.now())
			return updateErr
		}
		if response.State != "processing" {
			if err = s.finishRefundTx(ctx, tx, r, response.State); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `UPDATE v3_commerce.user_refunds SET provider_refund_id=$2,status=$3,last_error='',updated_at=$4 WHERE refund_no=$1`, no, response.RefundID, response.State, s.now())
		result.RefundID, result.Status = response.RefundID, response.State
		return err
	})
	return result, err
}

func (s *UserRefunds) finishRefundTx(ctx context.Context, tx pgx.Tx, r userRefundRecord, state string) error {
	var orderState string
	if err := tx.QueryRow(ctx, `SELECT state FROM v3_commerce.orders WHERE id=$1 FOR UPDATE`, r.orderID).Scan(&orderState); err != nil {
		return err
	}
	if orderState != "paid" {
		return ErrStateConflict
	}
	if state == "success" {
		_, err := tx.Exec(ctx, `UPDATE v3_commerce.orders SET state='refunded',refunded_at=$2 WHERE id=$1`, r.orderID, s.now())
		return err
	}
	if r.reserved > 0 {
		_, err := s.poster.PostTx(ctx, tx, billing.Entry{AccountID: r.accountID, Amount: r.reserved, Kind: "refund", OperationID: refundOperation(r.RefundNo) + ":release",
			Reason: "provider confirmed refund failure", Metadata: map[string]any{"refund_trade_no": r.TradeNo, "refund_no": r.RefundNo}})
		if err != nil {
			return err
		}
	}
	if r.subscriptionID > 0 {
		if err := lockSubscriptionUserTx(ctx, tx, r.subscriptionID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE v3_commerce.subscriptions SET state=$3,ended_at=NULL
		 WHERE id=$1 AND account_id=$2 AND state='canceled'`, r.subscriptionID, r.accountID, r.originalState)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrStateConflict
		}
		if err := ReapplySubscriptionGroupTx(ctx, tx, r.subscriptionID, s.now()); err != nil {
			return err
		}
	}
	return nil
}

// SyncPending completes requests whose caller disconnected or whose provider
// reply was ambiguous. Queries are throttled by the persisted update time.
func (s *UserRefunds) SyncPending(ctx context.Context) error {
	if s.provider == nil {
		return nil
	}
	rows, err := s.pool.Query(ctx, `SELECT user_id,refund_no FROM v3_commerce.user_refunds WHERE status='processing' AND updated_at<=$1 ORDER BY updated_at LIMIT 20`, s.now().Add(-30*time.Second))
	if err != nil {
		return err
	}
	type pending struct {
		uid int64
		no  string
	}
	var items []pending
	for rows.Next() {
		var p pending
		if err = rows.Scan(&p.uid, &p.no); err != nil {
			rows.Close()
			return err
		}
		items = append(items, p)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, p := range items {
		if _, err = s.Sync(ctx, p.uid, p.no); err != nil {
			return err
		}
	}
	return nil
}
