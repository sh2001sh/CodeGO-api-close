package commerce

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
)

// HandleWebhook admits an event only after the provider verifies its signature.
// The receipt, order transition and ledger grant either all commit or all roll back.
func (s *Service) HandleWebhook(ctx context.Context, provider string, header http.Header, body []byte) error {
	p := s.providers[provider]
	if p == nil {
		return ErrProviderUnavailable
	}
	e, err := p.Verify(ctx, header, body)
	if errors.Is(err, ErrIgnoredEvent) {
		return nil
	}
	if err != nil {
		return err
	}
	// Some hosted checkouts generate the provider order ID without echoing a
	// merchant trade number. Resolve only the signed, stored provider reference.
	if e.TradeNo == "" && e.Reference != "" {
		err = s.pool.QueryRow(ctx, `SELECT trade_no FROM v3_commerce.orders WHERE provider=$1 AND provider_reference=$2`, provider, e.Reference).Scan(&e.TradeNo)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
	}
	if e.Refunded {
		return s.ConfirmRefundTotal(ctx, provider, e.TradeNo, e.ID, e.Currency, e.AmountMinor)
	}
	if e.State != "" {
		return s.recordPaymentState(ctx, provider, e)
	}
	if e.ID == "" || e.TradeNo == "" || !e.Paid {
		return ErrInvalid
	}
	return s.Fulfill(ctx, provider, e)
}

// Fulfill accepts a previously verified payment event. Callers must not pass
// browser-submitted payment claims here; HTTP entry points use HandleWebhook.
func (s *Service) Fulfill(ctx context.Context, provider string, e PaymentEvent) error {
	if s.poster == nil {
		return ErrProviderUnavailable
	}
	if e.ID == "" || e.TradeNo == "" || !e.Paid {
		return ErrInvalid
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		o, err := scanOrder(tx.QueryRow(ctx, `SELECT `+orderColumns+` FROM v3_commerce.orders WHERE trade_no=$1 FOR UPDATE`, e.TradeNo))
		if err != nil {
			return err
		}
		if o.Provider != provider || o.AmountMinor != e.AmountMinor || o.Currency != e.Currency ||
			(o.ProviderReference != nil && *o.ProviderReference != e.Reference) {
			return ErrPaymentMismatch
		}
		if err = claimPaymentEvent(ctx, tx, provider, e); err != nil {
			return err
		}
		if o.State == "paid" {
			return nil
		}
		// Paid callbacks may arrive after a local expiration or cancellation. A
		// verified payment is still fulfilled; the provider is the payment authority.
		if o.State != "created" && o.State != "expired" && o.State != "canceled" && o.State != "failed" {
			return ErrStateConflict
		}
		// The paid-order inbox trigger takes a KEY SHARE lock on this user
		// through its notification FK. Acquire the lifecycle lock first so
		// concurrent callbacks cannot each retain that FK lock and then
		// deadlock when subscription fulfillment upgrades it to FOR UPDATE.
		if o.Kind == "subscription" {
			var lockedUser int64
			if err = tx.QueryRow(ctx, `SELECT id FROM v3_identity.users WHERE id=$1 FOR UPDATE`, o.UserID).Scan(&lockedUser); err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx, `UPDATE v3_commerce.orders SET state='paid',paid_at=$2,payment_event_id=$3,
		    provider_reference=COALESCE(provider_reference,NULLIF($4,''))
		    WHERE id=$1 AND state=$5`, o.ID, s.cfg.Now(), e.ID, e.Reference, o.State)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrStateConflict
		}
		return s.fulfillPaidOrderTx(ctx, tx, provider, o)
	})
}

// fulfillPaidOrderTx grants the order's benefit once its checkout discount
// reservation is confirmed consumed: a subscription grant, a cash box
// completion, or a plain wallet top-up, depending on order kind.
func (s *Service) fulfillPaidOrderTx(ctx context.Context, tx pgx.Tx, provider string, o Order) error {
	allowed, err := s.ConsumeCheckoutDiscountTx(ctx, tx, o)
	if err != nil {
		return err
	}
	if !allowed {
		return s.recordPackagePaymentReview(ctx, tx, o, "discount reservation released before verified payment")
	}
	if o.Kind == "subscription" {
		review, err := s.ValidateGroupFulfillmentTx(ctx, tx, o)
		if err != nil || review {
			return err
		}
		if err = s.grantSubscription(ctx, tx, o); err != nil {
			return err
		}
		if err = s.ApplyGroupCheckoutTx(ctx, tx, o); err != nil {
			return err
		}
		return s.applyPaidPurchaseTx(ctx, tx, o)
	}
	if o.Kind == "blind_box" {
		return s.CompleteCashBoxTx(ctx, tx, o)
	}
	account, err := walletTx(ctx, tx, o.UserID)
	if err != nil {
		return err
	}
	_, err = s.poster.PostTx(ctx, tx, billing.Entry{AccountID: account, Amount: o.Credits, Kind: "topup",
		OperationID: "order:paid:" + o.TradeNo, Reason: "verified payment", Metadata: map[string]any{"order_id": o.ID, "provider": provider}})
	if err != nil {
		return err
	}
	return s.applyPaidPurchaseTx(ctx, tx, o)
}

func claimPaymentEvent(ctx context.Context, tx pgx.Tx, provider string, e PaymentEvent) error {
	payload, err := json.Marshal(e)
	if err != nil {
		return err
	}
	fingerprint := sha256.Sum256(payload)
	var existing []byte
	err = tx.QueryRow(ctx, `INSERT INTO v3_commerce.payment_events(provider,event_id,trade_no,fingerprint)
	    VALUES($1,$2,$3,$4) ON CONFLICT(provider,event_id) DO NOTHING RETURNING fingerprint`, provider, e.ID, e.TradeNo, fingerprint[:]).Scan(&existing)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `SELECT fingerprint FROM v3_commerce.payment_events WHERE provider=$1 AND event_id=$2`, provider, e.ID).Scan(&existing)
	}
	if err != nil {
		return err
	}
	if string(existing) != string(fingerprint[:]) {
		return ErrPaymentMismatch
	}
	return nil
}

func (s *Service) recordPaymentState(ctx context.Context, provider string, e PaymentEvent) error {
	if e.ID == "" || e.TradeNo == "" || (e.State != "failed" && e.State != "expired") {
		return ErrInvalid
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		o, err := scanOrder(tx.QueryRow(ctx, `SELECT `+orderColumns+` FROM v3_commerce.orders WHERE trade_no=$1 FOR UPDATE`, e.TradeNo))
		if err != nil {
			return err
		}
		if o.Provider != provider || o.AmountMinor != e.AmountMinor || o.Currency != e.Currency ||
			(o.ProviderReference != nil && *o.ProviderReference != e.Reference) {
			return ErrPaymentMismatch
		}
		if err = claimPaymentEvent(ctx, tx, provider, e); err != nil {
			return err
		}
		if o.State != "created" {
			return nil
		}
		tag, err := tx.Exec(ctx, `UPDATE v3_commerce.orders SET state=$2 WHERE id=$1 AND state='created'`, o.ID, e.State)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrStateConflict
		}
		o.State = e.State
		return s.releaseCheckoutTx(ctx, tx, o)
	})
}

func walletTx(ctx context.Context, tx pgx.Tx, userID int64) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',$1,'wallet')
	    ON CONFLICT(owner_type,owner_id,kind) DO UPDATE SET owner_id=EXCLUDED.owner_id RETURNING id`, userID).Scan(&id)
	return id, err
}

// ConfirmRefund records a refund already confirmed by the payment provider.
// A chargeback can make the wallet negative; using adjustment records the full
// external loss rather than silently omitting it when credits were spent.
func (s *Service) ConfirmRefund(ctx context.Context, provider, tradeNo, refundID string) error {
	var amount int64
	var currency string
	err := s.pool.QueryRow(ctx, `SELECT amount_minor,currency FROM v3_commerce.orders WHERE trade_no=$1 AND provider=$2`, tradeNo, provider).Scan(&amount, &currency)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return s.ConfirmRefundTotal(ctx, provider, tradeNo, refundID, currency, amount)
}
