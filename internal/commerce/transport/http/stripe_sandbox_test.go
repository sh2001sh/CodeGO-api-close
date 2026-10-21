package http

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	commercestore "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"github.com/stretchr/testify/require"
)

// Opt-in: an isolated Stripe CLI sandbox forwards a genuine signed event. The
// caller supplies it in memory; no API key or production database is needed.
func TestStripeSandboxSignedDelivery(t *testing.T) {
	secret := os.Getenv("STRIPE_SANDBOX_WEBHOOK_SECRET")
	if secret == "" {
		t.Skip("requires an isolated Stripe sandbox delivery")
	}
	body := os.Getenv("STRIPE_SANDBOX_EVENT")
	var event struct {
		Livemode bool `json:"livemode"`
		Data     struct {
			Object struct {
				ID            string `json:"id"`
				Reference     string `json:"client_reference_id"`
				Currency      string `json:"currency"`
				Amount        int64  `json:"amount_total"`
				PaymentStatus string `json:"payment_status"`
			} `json:"object"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &event))
	session := event.Data.Object
	require.False(t, event.Livemode)
	require.True(t, strings.HasPrefix(session.Reference, "codego-sandbox-"))
	require.Equal(t, "paid", session.PaymentStatus)
	require.Contains(t, []string{"hkd", "cny"}, session.Currency)
	if session.Currency == "hkd" {
		require.EqualValues(t, 1500, session.Amount)
	} else {
		require.EqualValues(t, 1400, session.Amount)
	}
	db := setupCommerceHTTPTestDB(t)
	old := commercestore.StripeWebhookSecret
	commercestore.StripeWebhookSecret = secret
	t.Cleanup(func() { commercestore.StripeWebhookSecret = old })
	require.NoError(t, db.Create(&identityschema.User{Id: 877101, Username: "stripe-sandbox-fixture", Status: 1}).Error)
	payload, err := json.Marshal(map[string]any{"stripe_checkout": map[string]any{"version": 1, "currency": session.Currency, "amount": session.Amount, "live": false, "prop_id": 0, "session_id": session.ID}})
	require.NoError(t, err)
	require.NoError(t, db.Create(&commerceschema.TopUp{UserId: 877101, TradeNo: session.Reference, Money: float64(session.Amount) / 100, Amount: 10, PaidQuota: 5_000_000, PaymentProvider: "stripe", PaymentMethod: "stripe", Status: constant.TopUpStatusPending, ProviderPayload: string(payload)}).Error)
	for i := 0; i < 2; i++ {
		r := httptest.NewRequest("POST", "/api/stripe/webhook", bytes.NewBufferString(body))
		r.Header.Set("Stripe-Signature", os.Getenv("STRIPE_SANDBOX_SIGNATURE"))
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = r
		StripeWebhook(c)
		require.Equal(t, 200, w.Code)
	}
	var user identityschema.User
	require.NoError(t, db.First(&user, 877101).Error)
	require.EqualValues(t, 5_000_000, user.ClaudeQuota)
	t.Logf("genuine Stripe sandbox %s %0.2f payment: signature verified, two deliveries credited exactly 10 credits", strings.ToUpper(session.Currency), float64(session.Amount)/100)
}
