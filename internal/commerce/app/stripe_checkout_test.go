package app

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/constant"
	commercestore "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformpagination "github.com/sh2001sh/new-api/internal/platform/pagination"
	"github.com/stretchr/testify/require"
)

func stripePaidFixture(reference string, amount int64) map[string]any {
	return map[string]any{"id": "cs_test_currency", "client_reference_id": reference, "mode": "payment", "status": "complete", "payment_status": "paid", "payment_intent": "pi_fixture", "amount_total": amount, "currency": "cny", "livemode": false}
}

func TestStripeWalletCheckoutAndDuplicatePayment(t *testing.T) {
	db := setupRedemptionTestDB(t)
	require.NoError(t, db.AutoMigrate(&commerceschema.InvoiceRequest{}, &commerceschema.InvoiceRequestItem{}))
	confirmPaymentComplianceForTest(t)
	forms, _ := stubSubscriptionStripe(t, http.StatusOK)
	insertSubscriptionResetAppTestUser(t, 876001, 0)
	quote, err := QuoteStripeTopUpAmount(876001, StripePayRequest{Currency: "CNY", Amount: 10})
	require.NoError(t, err)
	require.Equal(t, "14.00", quote)
	checkout, err := CreateStripeTopUp(context.Background(), 876001, StripePayRequest{Currency: "CNY", Amount: 10, PaymentMethod: "stripe"})
	require.NoError(t, err)
	form := <-forms
	require.Equal(t, "cny", form.Get("line_items[0][price_data][currency]"))
	require.Equal(t, "1400", form.Get("line_items[0][price_data][unit_amount]"))
	require.Contains(t, form.Get("success_url"), "/wallet?pay=success")
	require.Empty(t, form.Get("payment_method_types[0]"))
	require.Empty(t, form.Get("allow_promotion_codes"))
	for i := 0; i < 2; i++ {
		require.NoError(t, HandleStripeWebhookFulfillment(context.Background(), checkout.OrderID, "cus_fixture", stripePaidFixture(checkout.OrderID, 1400), "127.0.0.1"))
	}
	var user identityschema.User
	require.NoError(t, db.First(&user, 876001).Error)
	require.EqualValues(t, quotaUnitsFromUSD(10), user.ClaudeQuota)
	invoice, err := loadInvoiceEligibleOrderTx(db, user.Id, "topup", checkout.OrderID)
	require.NoError(t, err)
	require.Equal(t, "CNY", invoice.Currency)
	require.Equal(t, 14.0, invoice.OrderAmount)
	page, err := ListInvoiceEligibleOrdersPage(user.Id, "cny", &platformpagination.PageInfo{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	var order commerceschema.TopUp
	require.NoError(t, db.Where("trade_no = ?", checkout.OrderID).First(&order).Error)
	require.Equal(t, "pi_fixture", order.ExternalPaymentID)
	require.Equal(t, constant.TopUpStatusSuccess, order.Status)
	t.Log("10 credits: CNY 14.00; two paid callbacks credited exactly 10 credits")
}

func TestStripePaymentMismatchDoesNotCredit(t *testing.T) {
	for _, field := range []string{"amount_total", "currency", "id", "client_reference_id", "mode", "status", "payment_status", "payment_intent", "livemode"} {
		t.Run(field, func(t *testing.T) {
			db := setupRedemptionTestDB(t)
			confirmPaymentComplianceForTest(t)
			forms, _ := stubSubscriptionStripe(t, http.StatusOK)
			insertSubscriptionResetAppTestUser(t, 876101, 0)
			checkout, err := CreateStripeTopUp(context.Background(), 876101, StripePayRequest{Currency: "CNY", Amount: 10, PaymentMethod: "stripe"})
			require.NoError(t, err)
			<-forms
			paid := stripePaidFixture(checkout.OrderID, 1400)
			switch field {
			case "amount_total":
				paid[field] = 999
			case "livemode":
				paid[field] = true
			default:
				paid[field] = "wrong"
			}
			require.Error(t, HandleStripeWebhookFulfillment(context.Background(), checkout.OrderID, "", paid, "127.0.0.1"))
			var user identityschema.User
			require.NoError(t, db.First(&user, 876101).Error)
			require.Zero(t, user.ClaudeQuota)
			var order commerceschema.TopUp
			require.NoError(t, db.Where("trade_no = ?", checkout.OrderID).First(&order).Error)
			require.Equal(t, constant.TopUpStatusPending, order.Status)
		})
	}
}

func TestStripeLatePaymentWithoutDiscountAndExpiry(t *testing.T) {
	db := setupRedemptionTestDB(t)
	confirmPaymentComplianceForTest(t)
	forms, _ := stubSubscriptionStripe(t, http.StatusOK)
	insertSubscriptionResetAppTestUser(t, 876201, 0)
	checkout, err := CreateStripeTopUp(context.Background(), 876201, StripePayRequest{Currency: "CNY", Amount: 10, PaymentMethod: "stripe"})
	require.NoError(t, err)
	<-forms
	require.NoError(t, db.Model(&commerceschema.TopUp{}).Where("trade_no = ?", checkout.OrderID).Update("create_time", time.Now().Add(-25*time.Hour).Unix()).Error)
	n, err := ExpireDueTopUps(300)
	require.NoError(t, err)
	require.Zero(t, n, "Stripe owns checkout expiry, including asynchronous methods")
	expired := stripePaidFixture(checkout.OrderID, 1400)
	expired["status"], expired["payment_status"] = "expired", "unpaid"
	require.NoError(t, ExpireStripeOrder(context.Background(), checkout.OrderID, expired))
	require.NoError(t, HandleStripeWebhookFulfillment(context.Background(), checkout.OrderID, "", stripePaidFixture(checkout.OrderID, 1400), "127.0.0.1"))
	var order commerceschema.TopUp
	require.NoError(t, db.Where("trade_no = ?", checkout.OrderID).First(&order).Error)
	require.Equal(t, constant.TopUpStatusSuccess, order.Status)
	// A late expiry delivery cannot reverse a paid order.
	require.NoError(t, ExpireStripeOrder(context.Background(), checkout.OrderID, expired))
}

func TestStripeStoredEvidenceSurvivesMutableSettings(t *testing.T) {
	payload := ""
	require.NoError(t, snapshotStripeCheckout(&payload, "cny", 64, 0))
	paid, err := json.Marshal(stripePaidFixture("ref_fixture", 6400))
	require.NoError(t, err)
	_, saved, err := validateStripePayment(payload, string(paid), "ref_fixture", 64)
	require.NoError(t, err)
	_, again, err := validateStripePayment(saved, string(paid), "ref_fixture", 64)
	require.NoError(t, err)
	require.JSONEq(t, saved, again)
	_, _, err = validateStripePayment(saved, string(paid), "ref_fixture", 65)
	require.Error(t, err)
	t.Log(fmt.Sprintf("immutable CNY snapshot rejects changed payable price"))
}

func TestStripeHKDWalletAndPackKeepApprovedCredits(t *testing.T) {
	db := setupRedemptionTestDB(t)
	require.NoError(t, db.AutoMigrate(&commerceschema.InvoiceRequest{}, &commerceschema.InvoiceRequestItem{}))
	confirmPaymentComplianceForTest(t)
	forms, _ := stubSubscriptionStripe(t, http.StatusOK)
	insertSubscriptionResetAppTestUser(t, 876301, 0)
	checkout, err := CreateStripeTopUp(context.Background(), 876301, StripePayRequest{Currency: "HKD", Amount: 10, PaymentMethod: "stripe"})
	require.NoError(t, err)
	require.Equal(t, "1500", (<-forms).Get("line_items[0][price_data][unit_amount]"))
	paid := stripePaidFixture(checkout.OrderID, 1500)
	paid["currency"] = "hkd"
	require.NoError(t, HandleStripeWebhookFulfillment(context.Background(), checkout.OrderID, "", paid, "127.0.0.1"))
	var user identityschema.User
	require.NoError(t, db.First(&user, 876301).Error)
	require.EqualValues(t, quotaUnitsFromUSD(10), user.ClaudeQuota)
	invoice, err := loadInvoiceEligibleOrderTx(db, user.Id, "topup", checkout.OrderID)
	require.NoError(t, err)
	require.Equal(t, "HKD", invoice.Currency)
	require.Equal(t, 15.0, invoice.OrderAmount)
	page, err := ListInvoiceEligibleOrdersPage(user.Id, "hkd", &platformpagination.PageInfo{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	plan := defaultSubscriptionPlans()[0]
	require.NoError(t, db.Create(&plan).Error)
	InvalidateSubscriptionPlanCache(plan.Id)
	t.Cleanup(func() { InvalidateSubscriptionPlanCache(plan.Id) })
	pack, err := CreateSubscriptionStripePayment(user.Id, SubscriptionStripePayRequest{PlanID: plan.Id, Currency: "HKD"})
	require.NoError(t, err)
	require.Equal(t, "HKD", pack.Currency)
	require.Equal(t, 96.0, pack.AmountDue)
	require.Equal(t, "9600", (<-forms).Get("line_items[0][price_data][unit_amount]"))
	paid = stripePaidFixture(pack.OrderID, 9600)
	paid["currency"] = "hkd"
	encoded, err := json.Marshal(paid)
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		require.NoError(t, CompleteSubscriptionOrder(pack.OrderID, string(encoded), "stripe", ""))
		require.NoError(t, FulfillPaidSubscriptionOrder(pack.OrderID))
	}
	var sub commerceschema.UserSubscription
	require.NoError(t, db.Where("user_id = ?", user.Id).First(&sub).Error)
	require.Equal(t, quotaUnitsFromUSD(67.2), sub.AmountTotal)
	require.Equal(t, quotaUnitsFromUSD(64), sub.CreditPackPaidQuota)
	require.Zero(t, sub.EndTime)
	t.Log("HKD 15 => 10 wallet credits; HKD 96 => one permanent 67.2-credit pack with unchanged 64-credit paid basis")
}

func TestStripePricingAndOrderBounds(t *testing.T) {
	for _, currency := range []string{"hkd", "cny"} {
		for _, amount := range []float64{0, -1, math.NaN(), math.Inf(1), math.MaxFloat64} {
			_, err := stripePriceFromCNY(amount, currency)
			require.Error(t, err)
		}
	}
	_, err := stripePresentmentCurrency("USD")
	require.Error(t, err)
	old := commercestore.StripeMinTopUp
	t.Cleanup(func() { commercestore.StripeMinTopUp = old })
	commercestore.StripeMinTopUp = 1
	for _, amount := range []int64{0, 1, 4, 10001, math.MaxInt64} {
		_, err := QuoteStripeTopUpAmount(1, StripePayRequest{Amount: amount, Currency: "HKD"})
		require.Error(t, err)
	}
}

func TestStripeUnrelatedExpiryDoesNotCancelOrder(t *testing.T) {
	db := setupRedemptionTestDB(t)
	confirmPaymentComplianceForTest(t)
	forms, _ := stubSubscriptionStripe(t, http.StatusOK)
	insertSubscriptionResetAppTestUser(t, 876401, 0)
	checkout, err := CreateStripeTopUp(context.Background(), 876401, StripePayRequest{Currency: "CNY", Amount: 10, PaymentMethod: "stripe"})
	require.NoError(t, err)
	<-forms
	expired := stripePaidFixture(checkout.OrderID, 1400)
	expired["payment_status"], expired["status"], expired["id"] = "unpaid", "expired", "cs_unrelated"
	require.Error(t, ExpireStripeOrder(context.Background(), checkout.OrderID, expired))
	var order commerceschema.TopUp
	require.NoError(t, db.Where("trade_no = ?", checkout.OrderID).First(&order).Error)
	require.Equal(t, constant.TopUpStatusPending, order.Status)
}
