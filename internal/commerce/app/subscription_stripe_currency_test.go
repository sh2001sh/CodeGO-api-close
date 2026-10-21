package app

import (
	"context"
	"fmt"
	"github.com/shopspring/decimal"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/constant"
	commercestore "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformgeneral "github.com/sh2001sh/new-api/internal/platform/general"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v87"
)

// Exercise the SDK's HTTP encoding without any requests to Stripe.
func stubSubscriptionStripe(t *testing.T, status int) (<-chan url.Values, *atomic.Int32) {
	t.Helper()
	forms := make(chan url.Values, 16)
	calls := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assert.True(t, strings.HasPrefix(r.Header.Get("Idempotency-Key"), "codego-checkout:"))
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/checkout/sessions", r.URL.Path)
		assert.Equal(t, "Bearer sk_test_subscription_currency_fixture", r.Header.Get("Authorization"))
		assert.NoError(t, r.ParseForm())
		forms <- r.PostForm
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status >= 400 {
			_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"checkout rejected"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"cs_test_currency","object":"checkout.session","url":"https://checkout.example.test/session"}`))
	}))
	t.Cleanup(server.Close)
	oldBackend := stripe.GetBackend(stripe.APIBackend)
	oldSecret, oldWebhook := commercestore.StripeApiSecret, commercestore.StripeWebhookSecret
	oldGlobalKey := stripe.Key
	stripe.SetBackend(stripe.APIBackend, stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{
		URL: stripe.String(server.URL), HTTPClient: server.Client(), MaxNetworkRetries: stripe.Int64(0),
	}))
	commercestore.StripeApiSecret, commercestore.StripeWebhookSecret = "sk_test_subscription_currency_fixture", "whsec_currency_fixture"
	stripe.Key = "global-key-must-remain-unchanged"
	t.Cleanup(func() {
		stripe.SetBackend(stripe.APIBackend, oldBackend)
		commercestore.StripeApiSecret, commercestore.StripeWebhookSecret = oldSecret, oldWebhook
		stripe.Key = oldGlobalKey
	})
	return forms, calls
}

func TestSubscriptionStripeCheckoutUsesPlanCurrency(t *testing.T) {
	tests := []struct {
		name, currency, wantCurrency string
		price                        float64
		pack                         bool
	}{
		{"pack64", "CNY", "cny", 64, true},
		{"pack128", "CNY", "cny", 128, true},
		{"pack500", "CNY", "cny", 500, true},
		{"pack1000", "CNY", "cny", 1000, true},
		{"pack3000", "CNY", "cny", 3000, true},
		{"legacyCNY", "CNY", "cny", 8.9, false},
		{"legacyUSD", "USD", "usd", 16.9, false},
		{"legacyDefaultUSD", "", "usd", 12.34, false},
		{"normalizedCNY", " cNy ", "cny", 12.34, false},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := setupRedemptionTestDB(t)
			forms, calls := stubSubscriptionStripe(t, http.StatusOK)
			userID := 872001 + i
			insertSubscriptionResetAppTestUser(t, userID, 0)
			plan := commerceschema.SubscriptionPlan{Id: 873001 + i, Title: tc.name, PriceAmount: tc.price, Currency: tc.currency, Enabled: true,
				PlanType: commerceschema.SubscriptionPlanTypeDaily, DurationUnit: commerceschema.SubscriptionDurationDay, DurationValue: 1, TotalAmount: 1000}
			if tc.pack {
				plan = defaultSubscriptionPlans()[i]
				plan.Id = 873001 + i
			}
			require.NoError(t, db.Create(&plan).Error)
			InvalidateSubscriptionPlanCache(plan.Id)
			t.Cleanup(func() { InvalidateSubscriptionPlanCache(plan.Id) })
			// Display preferences must never determine the payment currency.
			oldDisplay := platformgeneral.GetSetting().QuotaDisplayType
			t.Cleanup(func() { platformgeneral.GetSetting().QuotaDisplayType = oldDisplay })
			platformgeneral.GetSetting().QuotaDisplayType = platformgeneral.QuotaDisplayTypeUSD
			if tc.wantCurrency == "usd" {
				platformgeneral.GetSetting().QuotaDisplayType = platformgeneral.QuotaDisplayTypeCNY
				require.NoError(t, db.Model(&identityschema.User{}).Where("id = ?", userID).Update("stripe_customer", "cus_fixture").Error)
			}
			payload, err := CreateSubscriptionStripePayment(userID, SubscriptionStripePayRequest{PlanID: plan.Id, Currency: "CNY"})
			require.NoError(t, err)
			require.Equal(t, "https://checkout.example.test/session", payload.PayLink)
			base := tc.price
			if tc.wantCurrency == "usd" {
				base *= commercestore.USDExchangeRate
			}
			charged := decimal.NewFromFloat(base).Mul(decimal.NewFromFloat(commercestore.StripeCNYUnitPrice)).Round(2).InexactFloat64()
			require.Equal(t, charged, payload.AmountDue)
			form := <-forms
			require.Equal(t, "cny", form.Get("line_items[0][price_data][currency]"))
			require.Equal(t, strconv.FormatInt(int64(math.Round(charged*100)), 10), form.Get("line_items[0][price_data][unit_amount]"))
			require.Equal(t, payload.OrderID, form.Get("client_reference_id"))
			require.Equal(t, "1", form.Get("line_items[0][quantity]"))
			require.Equal(t, "payment", form.Get("mode"))
			require.NotContains(t, form, "payment_method_types[0]")
			require.Equal(t, "global-key-must-remain-unchanged", stripe.Key)
			require.EqualValues(t, 1, calls.Load())
			var order commerceschema.SubscriptionOrder
			require.NoError(t, db.Where("trade_no = ?", payload.OrderID).First(&order).Error)
			require.Equal(t, charged, order.Money)
			require.Equal(t, constant.TopUpStatusPending, order.Status)
			if tc.pack {
				require.Equal(t, plan.TotalAmount, order.CreditPackQuota)
				require.Equal(t, quotaUnitsFromUSD(tc.price), order.CreditPackPaidQuota)
			}
		})
	}
}

func TestGenStripeSubscriptionLinkRejectsInvalidMoneyAndCurrencyBeforeRequest(t *testing.T) {
	_, calls := stubSubscriptionStripe(t, http.StatusOK)
	for _, amount := range []float64{0, -1, 0.001, math.NaN(), math.Inf(1), math.Inf(-1), math.MaxFloat64} {
		_, err := genStripeSubscriptionLink("invalid", "", "", "fixture", "CNY", amount)
		require.Error(t, err)
	}
	for _, currency := range []string{"JPY", "EUR", "invalid"} {
		_, err := genStripeSubscriptionLink("invalid", "", "", "fixture", currency, 64)
		require.ErrorContains(t, err, "unsupported")
	}
	require.Zero(t, calls.Load())
}

func TestSubscriptionStripeCheckoutProviderFailureExpiresOrder(t *testing.T) {
	db := setupRedemptionTestDB(t)
	_, calls := stubSubscriptionStripe(t, http.StatusBadRequest)
	insertSubscriptionResetAppTestUser(t, 874001, 0)
	plan := defaultSubscriptionPlans()[0]
	plan.Id = 874002
	require.NoError(t, db.Create(&plan).Error)
	InvalidateSubscriptionPlanCache(plan.Id)
	t.Cleanup(func() { InvalidateSubscriptionPlanCache(plan.Id) })
	_, err := CreateSubscriptionStripePayment(874001, SubscriptionStripePayRequest{PlanID: plan.Id, Currency: "CNY"})
	require.Error(t, err)
	require.EqualValues(t, 1, calls.Load())
	var order commerceschema.SubscriptionOrder
	require.NoError(t, db.Where("user_id = ?", 874001).First(&order).Error)
	require.Equal(t, constant.TopUpStatusExpired, order.Status)
	require.Equal(t, quotaUnitsFromUSD(67.2), order.CreditPackQuota)
	var count int64
	require.NoError(t, db.Model(&commerceschema.UserSubscription{}).Where("user_id = ?", 874001).Count(&count).Error)
	require.Zero(t, count)
}

func TestSubscriptionStripeCheckoutUnsupportedCurrencyCreatesNoOrder(t *testing.T) {
	db := setupRedemptionTestDB(t)
	_, calls := stubSubscriptionStripe(t, http.StatusOK)
	insertSubscriptionResetAppTestUser(t, 874101, 0)
	plan := commerceschema.SubscriptionPlan{Id: 874102, Title: "Unsupported currency", PriceAmount: 64, Currency: "JPY", Enabled: true,
		PlanType: commerceschema.SubscriptionPlanTypeDaily, DurationUnit: commerceschema.SubscriptionDurationDay, DurationValue: 1, TotalAmount: 1000}
	require.NoError(t, db.Create(&plan).Error)
	InvalidateSubscriptionPlanCache(plan.Id)
	t.Cleanup(func() { InvalidateSubscriptionPlanCache(plan.Id) })
	_, err := CreateSubscriptionStripePayment(874101, SubscriptionStripePayRequest{PlanID: plan.Id, Currency: "CNY"})
	require.ErrorContains(t, err, "unsupported")
	require.Zero(t, calls.Load())
	var count int64
	require.NoError(t, db.Model(&commerceschema.SubscriptionOrder{}).Where("user_id = ?", 874101).Count(&count).Error)
	require.Zero(t, count)
}

func TestSubscriptionStripeCreditPackCallbackRemainsIdempotent(t *testing.T) {
	t.Setenv("TEMPORAL_HOSTPORT", "")
	db := setupRedemptionTestDB(t)
	_, _ = stubSubscriptionStripe(t, http.StatusOK)
	insertSubscriptionResetAppTestUser(t, 875001, 0)
	plan := defaultSubscriptionPlans()[0]
	plan.Id = 875002
	require.NoError(t, db.Create(&plan).Error)
	InvalidateSubscriptionPlanCache(plan.Id)
	t.Cleanup(func() { InvalidateSubscriptionPlanCache(plan.Id) })
	payload, err := CreateSubscriptionStripePayment(875001, SubscriptionStripePayRequest{PlanID: plan.Id, Currency: "CNY"})
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		require.NoError(t, HandleStripeWebhookFulfillment(context.Background(), payload.OrderID, "cus_fixture", map[string]any{"id": "cs_test_currency", "currency": "cny", "amount_total": 8960, "client_reference_id": payload.OrderID, "mode": "payment", "status": "complete", "payment_status": "paid", "payment_intent": "pi_test_currency", "livemode": false}, "127.0.0.1"))
		require.NoError(t, FulfillPaidSubscriptionOrder(payload.OrderID))
	}
	var count int64
	require.NoError(t, db.Model(&commerceschema.UserSubscription{}).Where("user_id = ?", 875001).Count(&count).Error)
	require.EqualValues(t, 1, count)
	var order commerceschema.SubscriptionOrder
	require.NoError(t, db.Where("trade_no = ?", payload.OrderID).First(&order).Error)
	require.Equal(t, constant.TopUpStatusSuccess, order.Status)
	require.Equal(t, commerceschema.SubscriptionOrderFulfillmentCompleted, order.FulfillmentStatus)
	var sub commerceschema.UserSubscription
	require.NoError(t, db.First(&sub, order.TargetSubscriptionId).Error)
	require.Equal(t, quotaUnitsFromUSD(67.2), sub.CreditPackGrantedQuota)
	require.Zero(t, sub.EndTime)
	t.Log(fmt.Sprintf("CNY 89.60 -> 8960 minor units; duplicate callbacks and fulfillment retained one permanent balance of %d quota", sub.CreditPackGrantedQuota))
}
