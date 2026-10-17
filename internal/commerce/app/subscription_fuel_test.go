package app

import (
	"testing"
	"time"

	"github.com/sh2001sh/new-api/constant"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func createSubscriptionFuelTestPlan(t *testing.T, id int) *commerceschema.SubscriptionPlan {
	t.Helper()
	plan := insertSubscriptionResetAppTestPlan(t, id, 0, quotaUnitsFromUSD(300))
	plan.Title = "Fuel Test Monthly Plan"
	plan.PlanType = commerceschema.SubscriptionPlanTypeMonthly
	plan.FuelEnabled = true
	plan.FuelUnitPrice = 0.17
	plan.FuelMinQuota = quotaUnitsFromUSD(10)
	plan.FuelQuotaStep = quotaUnitsFromUSD(10)
	require.NoError(t, platformdb.DB.Save(plan).Error)
	InvalidateSubscriptionPlanCache(plan.Id)
	return plan
}

func createSubscriptionFuelTestSubscription(t *testing.T, userID, subscriptionID, planID int, endTime int64) *commerceschema.UserSubscription {
	t.Helper()
	subscription := &commerceschema.UserSubscription{
		Id:          subscriptionID,
		UserId:      userID,
		PlanId:      planID,
		AmountTotal: quotaUnitsFromUSD(300),
		AmountUsed:  quotaUnitsFromUSD(250),
		StartTime:   time.Now().Add(-time.Hour).Unix(),
		EndTime:     endTime,
		Status:      "active",
	}
	require.NoError(t, platformdb.DB.Create(subscription).Error)
	return subscription
}

func TestQuoteSubscriptionFuel_UsesPlanSpecificRules(t *testing.T) {
	setupRedemptionTestDB(t)
	insertSubscriptionStoreTestUser(t, 9811, []int{9812})
	plan := createSubscriptionFuelTestPlan(t, 9813)
	subscription := createSubscriptionFuelTestSubscription(t, 9811, 9812, plan.Id, time.Now().Add(24*time.Hour).Unix())

	quote, err := QuoteSubscriptionFuel(9811, SubscriptionFuelQuoteRequest{
		SubscriptionID: subscription.Id,
		Quota:          quotaUnitsFromUSD(20),
	})
	require.NoError(t, err)
	assert.Equal(t, plan.Id, quote.PlanID)
	assert.Equal(t, plan.FuelUnitPrice, quote.UnitPrice)
	assert.Equal(t, 3.4, quote.AmountDue)
	assert.Equal(t, subscription.EndTime, quote.ExpiresAt)

	_, err = QuoteSubscriptionFuel(9811, SubscriptionFuelQuoteRequest{SubscriptionID: subscription.Id, Quota: quotaUnitsFromUSD(5)})
	require.ErrorIs(t, err, ErrSubscriptionFuelUnavailable)
	_, err = QuoteSubscriptionFuel(9811, SubscriptionFuelQuoteRequest{SubscriptionID: subscription.Id, Quota: quotaUnitsFromUSD(15)})
	require.ErrorIs(t, err, ErrSubscriptionFuelUnavailable)
}

func TestQuoteSubscriptionFuel_RejectsIneligibleSubscriptions(t *testing.T) {
	db := setupRedemptionTestDB(t)
	insertSubscriptionStoreTestUser(t, 9821, []int{9822, 9823, 9824})
	plan := createSubscriptionFuelTestPlan(t, 9825)
	active := createSubscriptionFuelTestSubscription(t, 9821, 9822, plan.Id, time.Now().Add(time.Hour).Unix())

	plan.FuelEnabled = false
	require.NoError(t, db.Save(plan).Error)
	InvalidateSubscriptionPlanCache(plan.Id)
	_, err := QuoteSubscriptionFuel(9821, SubscriptionFuelQuoteRequest{SubscriptionID: active.Id, Quota: quotaUnitsFromUSD(10)})
	require.ErrorIs(t, err, ErrSubscriptionFuelUnavailable)

	plan.FuelEnabled = true
	plan.PlanType = commerceschema.SubscriptionPlanTypeDaily
	plan.DurationUnit = commerceschema.SubscriptionDurationDay
	plan.DurationValue = 1
	require.NoError(t, db.Save(plan).Error)
	InvalidateSubscriptionPlanCache(plan.Id)
	_, err = QuoteSubscriptionFuel(9821, SubscriptionFuelQuoteRequest{SubscriptionID: active.Id, Quota: quotaUnitsFromUSD(10)})
	require.ErrorIs(t, err, ErrSubscriptionFuelUnavailable)

	plan.PlanType = commerceschema.SubscriptionPlanTypeMonthly
	plan.DurationUnit = commerceschema.SubscriptionDurationMonth
	plan.DurationValue = 1
	require.NoError(t, db.Save(plan).Error)
	InvalidateSubscriptionPlanCache(plan.Id)
	expired := createSubscriptionFuelTestSubscription(t, 9821, 9823, plan.Id, time.Now().Add(-time.Minute).Unix())
	_, err = QuoteSubscriptionFuel(9821, SubscriptionFuelQuoteRequest{SubscriptionID: expired.Id, Quota: quotaUnitsFromUSD(10)})
	require.ErrorIs(t, err, ErrSubscriptionFuelUnavailable)
}

func TestFulfillSubscriptionFuel_AddsQuotaOnceWithoutChangingExpiry(t *testing.T) {
	db := setupRedemptionTestDB(t)
	insertSubscriptionStoreTestUser(t, 9831, []int{9832})
	plan := createSubscriptionFuelTestPlan(t, 9833)
	subscription := createSubscriptionFuelTestSubscription(t, 9831, 9832, plan.Id, time.Now().Add(24*time.Hour).Unix())
	order := &commerceschema.SubscriptionOrder{
		UserId:               subscription.UserId,
		PlanId:               plan.Id,
		Money:                3.4,
		TradeNo:              "subscription-fuel-fulfillment",
		PaymentMethod:        "test",
		PaymentProvider:      "test",
		PurchaseType:         commerceschema.SubscriptionPurchaseTypeFuel,
		TargetSubscriptionId: subscription.Id,
		FuelQuota:            quotaUnitsFromUSD(20),
		FuelUnitPrice:        plan.FuelUnitPrice,
		FuelExpiresAt:        subscription.EndTime,
		Status:               constant.TopUpStatusSuccess,
		FulfillmentStatus:    commerceschema.SubscriptionOrderFulfillmentPending,
		CreateTime:           platformruntime.GetTimestamp(),
	}
	require.NoError(t, db.Create(order).Error)

	require.NoError(t, FulfillPaidSubscriptionOrder(order.TradeNo))
	require.NoError(t, FulfillPaidSubscriptionOrder(order.TradeNo))

	var reloadedSubscription commerceschema.UserSubscription
	require.NoError(t, db.Where("id = ?", subscription.Id).First(&reloadedSubscription).Error)
	assert.Equal(t, quotaUnitsFromUSD(320), reloadedSubscription.AmountTotal)
	assert.Equal(t, subscription.EndTime, reloadedSubscription.EndTime)

	var account billingschema.BillingAccount
	require.NoError(t, db.Where("owner_type = ? AND owner_id = ? AND account_type = ?", "user_subscription", subscription.Id, "subscription").First(&account).Error)
	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.Where("account_id = ?", account.AccountID).First(&snapshot).Error)
	assert.Equal(t, quotaUnitsFromUSD(70), snapshot.AvailableBalance)

	var reloadedOrder commerceschema.SubscriptionOrder
	require.NoError(t, db.Where("trade_no = ?", order.TradeNo).First(&reloadedOrder).Error)
	assert.Equal(t, commerceschema.SubscriptionOrderFulfillmentCompleted, reloadedOrder.FulfillmentStatus)
}

func TestFulfillSubscriptionFuelTx_MaterializesOnlyUnbackedBalance(t *testing.T) {
	for _, tc := range []struct {
		name          string
		existing      bool
		ledgerAmount  int64
		ledgerMarker  bool
		used          int64
		wantAvailable int64
		wantBaseline  int64
	}{
		{name: "new account", used: 600, wantAvailable: 600, wantBaseline: 400},
		{name: "empty account", existing: true, used: 600, wantAvailable: 600, wantBaseline: 400},
		{name: "authoritative ledger", existing: true, ledgerAmount: 75, used: 600, wantAvailable: 275},
		{name: "zero balance authoritative ledger", existing: true, ledgerMarker: true, used: 600, wantAvailable: 200},
		{name: "exhausted projection", used: 1000, wantAvailable: 200},
		{name: "overspent projection", used: 1100, wantAvailable: 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupRedemptionTestDB(t)
			sub := &commerceschema.UserSubscription{Id: 9842, UserId: 9841, PlanId: 9843, AmountTotal: 1000, AmountUsed: tc.used, Status: "active", EndTime: time.Now().Add(time.Hour).Unix()}
			require.NoError(t, db.Create(sub).Error)
			if tc.existing {
				account, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{AccountType: "subscription", OwnerType: "user_subscription", OwnerID: int64(sub.Id), QuotaUnit: "quota"})
				require.NoError(t, err)
				if tc.ledgerAmount > 0 {
					_, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{AccountID: account.AccountID, Amount: tc.ledgerAmount, IdempotencyKey: "fuel-authoritative"})
					require.NoError(t, err)
				}
				if tc.ledgerMarker {
					_, err = billingdomain.RecordAdjustment(billingdomain.RecordAdjustmentParams{AccountID: account.AccountID, IdempotencyKey: "fuel-authoritative-zero"})
					require.NoError(t, err)
				}
			}
			order := &commerceschema.SubscriptionOrder{UserId: sub.UserId, TradeNo: "fuel-materialize", PurchaseType: commerceschema.SubscriptionPurchaseTypeFuel, TargetSubscriptionId: sub.Id, FuelQuota: 200, FuelExpiresAt: sub.EndTime}
			fulfill := func() error {
				return db.Transaction(func(tx *gorm.DB) error { return fulfillSubscriptionFuelTx(tx, order) })
			}
			require.NoError(t, fulfill())
			require.NoError(t, fulfill())

			var reloaded commerceschema.UserSubscription
			require.NoError(t, db.First(&reloaded, sub.Id).Error)
			assert.EqualValues(t, 1200, reloaded.AmountTotal)
			assert.Equal(t, tc.used, reloaded.AmountUsed)
			var account billingschema.BillingAccount
			require.NoError(t, db.Where("owner_type = ? AND owner_id = ?", "user_subscription", sub.Id).First(&account).Error)
			var snapshot billingschema.BillingBalanceSnapshot
			require.NoError(t, db.First(&snapshot, "account_id = ?", account.AccountID).Error)
			assert.Equal(t, tc.wantAvailable, snapshot.AvailableBalance)
			var baseline int64
			require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Where("account_id = ? AND reason_code = ?", account.AccountID, "subscription_balance_bootstrap").Select("COALESCE(SUM(amount), 0)").Scan(&baseline).Error)
			assert.Equal(t, tc.wantBaseline, baseline)
			var fuels int64
			require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Where("idempotency_key = ?", "subscription-fuel:"+order.TradeNo).Count(&fuels).Error)
			assert.EqualValues(t, 1, fuels)

			order.FuelQuota++
			require.ErrorIs(t, fulfill(), billingdomain.ErrLedgerConflict)
			require.NoError(t, db.First(&snapshot, "account_id = ?", account.AccountID).Error)
			assert.Equal(t, tc.wantAvailable, snapshot.AvailableBalance)
		})
	}
}

func TestFulfillSubscriptionFuelTx_RollsBackMaterializationWithProjection(t *testing.T) {
	db := setupRedemptionTestDB(t)
	sub := &commerceschema.UserSubscription{Id: 9852, UserId: 9851, PlanId: 9853, AmountTotal: 1000, AmountUsed: 600, Status: "active", EndTime: time.Now().Add(time.Hour).Unix()}
	require.NoError(t, db.Create(sub).Error)
	account, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{AccountType: "subscription", OwnerType: "user_subscription", OwnerID: int64(sub.Id), QuotaUnit: "quota"})
	require.NoError(t, err)
	order := &commerceschema.SubscriptionOrder{UserId: sub.UserId, TradeNo: "fuel-rollback", PurchaseType: commerceschema.SubscriptionPurchaseTypeFuel, TargetSubscriptionId: sub.Id, FuelQuota: 200, FuelExpiresAt: sub.EndTime}
	require.NoError(t, db.Exec("CREATE TRIGGER reject_fuel_projection BEFORE UPDATE ON user_subscriptions BEGIN SELECT RAISE(ABORT, 'reject fuel projection'); END").Error)
	require.ErrorContains(t, db.Transaction(func(tx *gorm.DB) error { return fulfillSubscriptionFuelTx(tx, order) }), "reject fuel projection")
	var entries, outbox int64
	require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Count(&entries).Error)
	require.NoError(t, db.Model(&billingschema.BillingOutboxEvent{}).Count(&outbox).Error)
	assert.Zero(t, entries)
	assert.Zero(t, outbox)
	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.First(&snapshot, "account_id = ?", account.AccountID).Error)
	assert.Zero(t, snapshot.AvailableBalance)
	var reloaded commerceschema.UserSubscription
	require.NoError(t, db.First(&reloaded, sub.Id).Error)
	assert.EqualValues(t, 1000, reloaded.AmountTotal)
	require.NoError(t, db.Exec("DROP TRIGGER reject_fuel_projection").Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return fulfillSubscriptionFuelTx(tx, order) }))
	require.NoError(t, db.First(&snapshot, "account_id = ?", account.AccountID).Error)
	assert.EqualValues(t, 600, snapshot.AvailableBalance)
}
