package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	commercestore "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	"github.com/stripe/stripe-go/v87"
)

func refundPaymentCurrency(provider, payload string) string {
	if provider == commerceschema.PaymentProviderStripe {
		var paid stripePaymentEvidence
		if json.Unmarshal([]byte(payload), &paid) == nil {
			if paid.Checkout != nil && (paid.Checkout.Currency == "hkd" || paid.Checkout.Currency == "cny") {
				return strings.ToUpper(paid.Checkout.Currency)
			}
		}
		return "USD"
	}
	return "CNY"
}

func stripeRefundEvidence(payload string) (*stripePaymentEvidence, error) {
	var paid stripePaymentEvidence
	if json.Unmarshal([]byte(payload), &paid) != nil || paid.Checkout == nil || !strings.HasPrefix(paid.PaymentIntent, "pi_") {
		return nil, errors.New("missing Stripe original payment")
	}
	live := strings.HasPrefix(commercestore.StripeApiSecret, "sk_live_") || strings.HasPrefix(commercestore.StripeApiSecret, "rk_live_")
	if live != paid.Live {
		return nil, errors.New("Stripe refund environment mismatch")
	}
	return &paid, nil
}

// Adapt Stripe's response to the existing native refund state machine. Its
// reservation, administrator authorization and exact-attempt checks stay intact.
func stripeRefundResult(refund *stripe.Refund, paid *stripePaymentEvidence, refundNo string, amount int64) (*JianPayRefundResult, error) {
	if refund == nil || refund.ID == "" || refund.PaymentIntent == nil || refund.PaymentIntent.ID != paid.PaymentIntent || refund.Currency != stripe.Currency(paid.Currency) || refund.Amount != amount || refund.Metadata["codego_refund"] != refundNo {
		return nil, errors.New("Stripe refund does not match original attempt")
	}
	status := 1
	switch refund.Status {
	case stripe.RefundStatusSucceeded:
		status = 2
	case stripe.RefundStatusFailed, stripe.RefundStatusCanceled:
		status = 3
	}
	return &JianPayRefundResult{RefundID: refund.ID, RefundNo: refundNo, OrderID: paid.PaymentIntent, RefundAmount: refund.Amount, Status: status}, nil
}

func createStripeRefund(payload, refundNo string, amount int64) (*JianPayRefundResult, error) {
	paid, err := stripeRefundEvidence(payload)
	if err != nil {
		return nil, err
	}
	if amount <= 0 || amount > paid.Amount {
		return nil, errors.New("invalid Stripe refund amount")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	params := &stripe.RefundCreateParams{PaymentIntent: stripe.String(paid.PaymentIntent), Amount: stripe.Int64(amount), Reason: stripe.String("requested_by_customer"), Metadata: map[string]string{"codego_refund": refundNo, "codego_order": paid.Reference}}
	params.SetIdempotencyKey("codego-refund:" + refundNo)
	refund, err := stripe.NewClient(commercestore.StripeApiSecret).V1Refunds.Create(ctx, params)
	if err != nil {
		return nil, err
	}
	return stripeRefundResult(refund, paid, refundNo, amount)
}

func queryStripeRefund(payload, providerID, refundNo string, amount int64) (*JianPayRefundResult, error) {
	paid, err := stripeRefundEvidence(payload)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	client := stripe.NewClient(commercestore.StripeApiSecret)
	if providerID != "" {
		refund, err := client.V1Refunds.Retrieve(ctx, providerID, nil)
		if err != nil {
			return nil, err
		}
		return stripeRefundResult(refund, paid, refundNo, amount)
	}
	// Lost create responses are recovered by immutable metadata, never by
	// issuing a second refund after Stripe's idempotency retention has elapsed.
	var found *stripe.Refund
	params := &stripe.RefundListParams{PaymentIntent: stripe.String(paid.PaymentIntent)}
	params.Limit = stripe.Int64(100)
	for refund, err := range client.V1Refunds.List(ctx, params).All(ctx) {
		if err != nil {
			return nil, err
		}
		if refund.Metadata["codego_refund"] == refundNo {
			if found != nil {
				return nil, errors.New("multiple Stripe refunds match this attempt")
			}
			found = refund
		}
	}
	if found == nil {
		return nil, errors.New("Stripe refund result not yet confirmed; keep the original hold")
	}
	return stripeRefundResult(found, paid, refundNo, amount)
}
