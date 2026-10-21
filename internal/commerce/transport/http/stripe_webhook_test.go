package http

import (
	"bytes"
	"encoding/json"
	"fmt"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	commercestore "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v87"
	"github.com/stripe/stripe-go/v87/webhook"
)

func TestStripeWebhookSignatureRetryAndDuplicate(t *testing.T) {
	db := setupCommerceHTTPTestDB(t)
	old := commercestore.StripeWebhookSecret
	commercestore.StripeWebhookSecret = "whsec_fixture_only"
	t.Cleanup(func() { commercestore.StripeWebhookSecret = old })
	user := identityschema.User{Id: 877001, Username: "stripe-http-fixture", Status: 1}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&commerceschema.TopUp{UserId: user.Id, TradeNo: "ref_http", Amount: 10, Money: 10, PaidQuota: 5_000_000, Status: constant.TopUpStatusPending, PaymentProvider: "stripe", PaymentMethod: "stripe", WalletType: "claude", ProviderPayload: `{"stripe_checkout":{"version":1,"currency":"cny","amount":1000,"live":false,"prop_id":0,"session_id":"cs_test_http"}}`}).Error)
	paid := map[string]any{"id": "cs_test_http", "object": "checkout.session", "client_reference_id": "ref_http", "mode": "payment", "status": "complete", "payment_status": "paid", "amount_total": 1000, "currency": "cny", "livemode": false, "payment_intent": "pi_http"}
	send := func(eventType string, signatureValid bool) int {
		t.Helper()
		body, err := json.Marshal(map[string]any{"id": "evt_http", "object": "event", "type": eventType, "api_version": stripe.APIVersion, "data": map[string]any{"object": paid}})
		require.NoError(t, err)
		signed := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{Payload: body, Secret: commercestore.StripeWebhookSecret})
		r := httptest.NewRequest(http.MethodPost, "/api/stripe/webhook", bytes.NewReader(body))
		if signatureValid {
			r.Header.Set("Stripe-Signature", signed.Header)
		} else {
			r.Header.Set("Stripe-Signature", "invalid")
		}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = r
		StripeWebhook(c)
		return w.Code
	}
	getQuota := func() int {
		var u identityschema.User
		require.NoError(t, db.First(&u, user.Id).Error)
		return u.ClaudeQuota
	}
	require.Equal(t, 400, send("checkout.session.completed", false))
	require.Zero(t, getQuota())
	paid["payment_status"] = "unpaid"
	require.Equal(t, 200, send("checkout.session.async_payment_succeeded", true))
	require.Zero(t, getQuota())
	paid["payment_status"] = "paid"
	paid["amount_total"] = 999
	require.Equal(t, 503, send("checkout.session.completed", true))
	require.Zero(t, getQuota())
	paid["amount_total"] = 1000
	// A transaction failure must be retryable, with no partial wallet credit.
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("stripe_fail_ledger", func(tx *gorm.DB) {
		if tx.Statement.Table == "billing_ledger_entries" {
			tx.AddError(fmt.Errorf("injected write failure"))
		}
	}))
	require.Equal(t, 503, send("checkout.session.completed", true))
	require.Zero(t, getQuota())
	require.NoError(t, db.Callback().Create().Remove("stripe_fail_ledger"))
	require.Equal(t, 200, send("checkout.session.async_payment_succeeded", true))
	require.EqualValues(t, 5_000_000, getQuota())
	require.Equal(t, 200, send("checkout.session.completed", true))
	require.EqualValues(t, 5_000_000, getQuota())
	t.Log("invalid signature 400; unpaid 200/zero credit; amount mismatch and write failure 503; retry and duplicate 200/exactly one credit")
}
