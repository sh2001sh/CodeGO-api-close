package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	commercestore "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v87"
)

func TestStripeRefundUnknownResponseRecoveredWithoutSecondCreate(t *testing.T) {
	for _, finalStatus := range []string{"succeeded", "failed"} {
		t.Run(finalStatus, func(t *testing.T) {
			db := setupRedemptionTestDB(t)
			order, account := seedRefundWallet(t, db)
			oldKey := commercestore.StripeApiSecret
			commercestore.StripeApiSecret = "rk_test_refund_fixture"
			t.Cleanup(func() { commercestore.StripeApiSecret = oldKey })
			payload := ""
			require.NoError(t, snapshotStripeCheckout(&payload, "hkd", 150, 0))
			paid := stripePaidFixture(order.TradeNo, 15000)
			paid["currency"] = "hkd"
			encoded, err := json.Marshal(paid)
			require.NoError(t, err)
			_, payload, err = validateStripePayment(payload, string(encoded), order.TradeNo, 150)
			require.NoError(t, err)
			require.NoError(t, db.Model(order).Updates(map[string]any{"payment_provider": "stripe", "payment_method": "stripe", "money": 150, "provider_payload": payload}).Error)
			creates, queries := 0, 0
			refundNo := ""
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assertRefundWallet(t, db, account, 0, 1000, 0)
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodPost {
					creates++
					require.NoError(t, r.ParseForm())
					refundNo = r.Form.Get("metadata[codego_refund]")
					require.Equal(t, "codego-refund:"+refundNo, r.Header.Get("Idempotency-Key"))
					require.Equal(t, "pi_fixture", r.Form.Get("payment_intent"))
					require.Equal(t, "14700", r.Form.Get("amount"))
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"error":{"type":"api_error","message":"response lost"}}`))
					return
				}
				queries++
				require.Equal(t, "pi_fixture", r.URL.Query().Get("payment_intent"))
				require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"object": "list", "has_more": false, "data": []any{map[string]any{"id": "re_fixture", "object": "refund", "payment_intent": "pi_fixture", "amount": 14700, "currency": "hkd", "status": finalStatus, "metadata": map[string]string{"codego_refund": refundNo}}}}))
			}))
			t.Cleanup(server.Close)
			oldBackend := stripe.GetBackend(stripe.APIBackend)
			stripe.SetBackend(stripe.APIBackend, stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{URL: stripe.String(server.URL), HTTPClient: server.Client(), MaxNetworkRetries: stripe.Int64(0)}))
			t.Cleanup(func() { stripe.SetBackend(stripe.APIBackend, oldBackend) })
			result, err := createRefund(order.UserId, commerceschema.RefundRequest{OrderType: "balance", TradeNo: order.TradeNo})
			require.Error(t, err)
			require.Equal(t, "processing", result.Status)
			assertRefundWallet(t, db, account, 0, 1000, 0)
			_, err = createRefund(order.UserId, commerceschema.RefundRequest{OrderType: "balance", TradeNo: order.TradeNo})
			require.Error(t, err)
			result, err = syncRefund(order.UserId, result.RefundNo)
			require.NoError(t, err)
			if finalStatus == "succeeded" {
				require.Equal(t, "success", result.Status)
				assertRefundWallet(t, db, account, 0, 0, 0)
			} else {
				require.Equal(t, "failed", result.Status)
				assertRefundWallet(t, db, account, 1000, 0, 1000)
			}
			require.Equal(t, 1, creates)
			require.Equal(t, 1, queries)
		})
	}
}

func TestStripeRefundRejectsDifferentAttempt(t *testing.T) {
	paid := &stripePaymentEvidence{PaymentIntent: "pi_original", Currency: "hkd"}
	base := stripe.Refund{ID: "re_fixture", PaymentIntent: &stripe.PaymentIntent{ID: "pi_original"}, Currency: "hkd", Amount: 14700, Status: "succeeded", Metadata: map[string]string{"codego_refund": "original"}}
	for _, field := range []string{"amount", "currency", "intent", "metadata"} {
		t.Run(field, func(t *testing.T) {
			refund := base
			switch field {
			case "amount":
				refund.Amount++
			case "currency":
				refund.Currency = "cny"
			case "intent":
				refund.PaymentIntent = &stripe.PaymentIntent{ID: "pi_other"}
			case "metadata":
				refund.Metadata = map[string]string{"codego_refund": "other"}
			}
			_, err := stripeRefundResult(&refund, paid, "original", 14700)
			require.Error(t, err)
		})
	}
}
