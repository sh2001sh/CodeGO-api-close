package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const importedCashBoxReview = "legacy_cash_box_review"

// quarantineImportedCashBoxPayment receives only an already verified payment.
// V2 cash orders did not freeze their draw policy, so a late payment cannot be
// fulfilled using the current pool. Record the original money for review, with
// no new checkout, wallet grant, inventory or reward-policy interpretation.
// A previously delivered V2 order acknowledges only its exact original receipt.
func (s *Service) quarantineImportedCashBoxPayment(ctx context.Context, provider string, e PaymentEvent) (bool, error) {
	if provider != "epay" || !e.Paid {
		return false, nil
	}
	handled := false
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		o, orderErr := scanOrder(tx.QueryRow(ctx, `SELECT `+orderColumns+` FROM v3_commerce.orders WHERE trade_no=$1 FOR UPDATE`, e.TradeNo))
		if orderErr == nil && o.PurchaseType != importedCashBoxReview {
			return nil
		}
		if orderErr != nil && !errors.Is(orderErr, ErrNotFound) {
			return orderErr
		}
		var userID, amount int64
		var currency, originalProvider, method, status string
		var created, closed time.Time
		var payload json.RawMessage
		var originalDeliveryComplete bool
		err := tx.QueryRow(ctx, `SELECT user_id,amount_minor,currency,payment_provider,payment_method,status,created_at,coalesce(completed_at,created_at),provider_payload,
		 completed_at IS NOT NULL AND opened_count=quantity
		 FROM v3_marketplace.blind_box_orders WHERE trade_no=$1 AND source='purchase' AND amount_minor>0 FOR UPDATE`, e.TradeNo).
			Scan(&userID, &amount, &currency, &originalProvider, &method, &status, &created, &closed, &payload, &originalDeliveryComplete)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if originalProvider != provider || currency != e.Currency || amount != e.AmountMinor || e.Reference != e.TradeNo {
			return ErrPaymentMismatch
		}
		// Another first callback may have created the financial record while we
		// waited on the original order row. Re-read under that shared row lock.
		if errors.Is(orderErr, ErrNotFound) {
			o, orderErr = scanOrder(tx.QueryRow(ctx, `SELECT `+orderColumns+` FROM v3_commerce.orders WHERE trade_no=$1 FOR UPDATE`, e.TradeNo))
		}
		if orderErr == nil {
			if o.PurchaseType != importedCashBoxReview || o.Kind != "blind_box" || o.UserID != userID || o.Provider != provider || o.Currency != currency || o.AmountMinor != amount {
				return ErrPaymentMismatch
			}
			var identity string
			if err = tx.QueryRow(ctx, `SELECT payment_event_id FROM v3_commerce.orders WHERE id=$1`, o.ID).Scan(&identity); err != nil {
				return err
			}
			if identity != e.ID {
				return ErrPaymentMismatch
			}
			if err = claimPaymentEvent(ctx, tx, provider, e); err != nil {
				return err
			}
			handled = true
			return nil
		}
		if !errors.Is(orderErr, ErrNotFound) {
			return orderErr
		}
		if status == "success" || status == "completed" {
			// The source saved this verified identity in the same transaction
			// that issued its inventory. A new event cannot impersonate that
			// historical receipt. No financial order or delivery is repeated.
			var original struct {
				TradeNo, ServiceTradeNo, Money, TradeStatus string
				VerifyStatus                                bool
			}
			if err := json.Unmarshal(payload, &original); err != nil {
				return ErrPaymentMismatch
			}
			paid, err := parseMinor(original.Money)
			if err != nil || !originalDeliveryComplete || !original.VerifyStatus ||
				(original.TradeStatus != "TRADE_SUCCESS" && original.TradeStatus != "TRADE_FINISHED") ||
				original.TradeNo != e.ID || original.ServiceTradeNo != e.TradeNo || paid != amount {
				return ErrPaymentMismatch
			}
			handled = true
			return nil
		}
		if status != "pending" && status != "expired" && status != "failed" && status != "canceled" && status != "cancelled" {
			return nil
		}
		o, err = scanOrder(tx.QueryRow(ctx, `INSERT INTO v3_commerce.orders
		 (user_id,amount_minor,credits,currency,kind,provider,trade_no,state,provider_reference,payment_event_id,
		 created_at,expires_at,paid_at,fulfillment_state,purchase_type,checkout_selection)
		 VALUES($1,$2,0,$3,'blind_box',$4,$5,'paid',$5,$6,$7,$8,$9,'requires_review',$10,$11)
		 RETURNING `+orderColumns, userID, amount, currency, provider, e.TradeNo, e.ID, created, closed, s.cfg.Now(), importedCashBoxReview, CheckoutSelection{PaymentMethod: method}))
		if err != nil {
			return err
		}
		if err = claimPaymentEvent(ctx, tx, provider, e); err != nil {
			return err
		}
		reason := fmt.Sprintf("Legacy cash blind-box payment received; provider transaction %s; %d %s minor units. Original draw rights require manual verification; no wallet credits or inventory issued.", e.ID, amount, currency)
		if err = s.recordPackagePaymentReview(ctx, tx, o, reason); err != nil {
			return err
		}
		receipt, err := json.Marshal(map[string]any{"event_id": e.ID, "trade_no": e.TradeNo, "provider": provider, "amount_minor": amount, "currency": currency, "received_at": s.cfg.Now(), "fulfillment_state": "requires_review"})
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_orders SET provider_payload=jsonb_set(provider_payload,'{v3_late_payment}',$2::jsonb,true) WHERE trade_no=$1`, e.TradeNo, receipt); err != nil {
			return err
		}
		// The owner and every active administrator receive one durable inbox
		// record. No transient log or external message substitutes for review.
		_, err = tx.Exec(ctx, `INSERT INTO v3_identity.notifications(user_id,dedupe_key,category,kind,title_key,body_key,data,action_url)
		 SELECT id,'legacy-cash-box-review:'||$1||':'||id::text,'review','legacy_cash_box_payment_review',
		 'notifications.legacyCashBoxReview','notifications.legacyCashBoxReviewBody',
		 jsonb_build_object('order_id',$2::text,'trade_no',$1::text,'provider_event_id',$3::text,'amount_minor',$4::text,'currency',$5::text),
		 CASE WHEN id=$6 THEN '/orders' ELSE '/admin/orders' END
		 FROM v3_identity.users WHERE id=$6 OR (role IN('admin','root') AND status='active' AND deleted_at IS NULL)
		 ON CONFLICT(dedupe_key) DO NOTHING`, e.TradeNo, fmt.Sprint(o.ID), e.ID, fmt.Sprint(amount), currency, userID)
		if err != nil {
			return err
		}
		handled = true
		return nil
	})
	return handled, err
}
