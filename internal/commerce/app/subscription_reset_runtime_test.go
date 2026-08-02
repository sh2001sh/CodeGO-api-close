package app

import (
	"fmt"
	"github.com/sh2001sh/new-api/constant"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"testing"
	"time"
)

func insertSubscriptionResetAppTestUser(t *testing.T, id int, inviterID int) {
	t.Helper()
	user := &identityschema.User{
		Id:        id,
		Username:  fmt.Sprintf("subscription_reset_app_test_user_%d", id),
		Status:    constant.UserStatusEnabled,
		AffCode:   fmt.Sprintf("AFF-%d", id),
		InviterId: inviterID,
	}
	require.NoError(t, platformdb.DB.Create(user).Error)
}

func insertSubscriptionResetAppTestPlan(t *testing.T, id int, durationDays int, totalAmount int64) *commerceschema.SubscriptionPlan {
	t.Helper()
	plan := &commerceschema.SubscriptionPlan{
		Id:            id,
		Title:         "Subscription Reset Test Plan",
		PriceAmount:   50,
		Currency:      "CNY",
		DurationUnit:  commerceschema.SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   totalAmount,
	}
	if durationDays > 0 {
		plan.DurationUnit = commerceschema.SubscriptionDurationDay
		plan.DurationValue = durationDays
	}
	require.NoError(t, platformdb.DB.Create(plan).Error)
	return plan
}

func TestAwardReferralSubscriptionResetOpportunityTx_MonthCardAwardedOnce(t *testing.T) {
	db := setupRedemptionTestDB(t)

	insertSubscriptionResetAppTestUser(t, 7101, 0)
	insertSubscriptionResetAppTestUser(t, 7102, 7101)

	err := db.Transaction(func(tx *gorm.DB) error {
		return AwardReferralSubscriptionResetOpportunityTx(
			tx,
			7102,
			commercedomain.ReferralPurchaseTypeMonthCard,
			"subscription_order",
			"trade-7102",
		)
	})
	require.NoError(t, err)

	summary, err := GetUserSubscriptionResetOpportunity(7101)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.AvailableCount)
	assert.Equal(t, 1, summary.EarnedTotal)
	assert.Equal(t, 0, summary.UsedTotal)

	err = db.Transaction(func(tx *gorm.DB) error {
		return AwardReferralSubscriptionResetOpportunityTx(
			tx,
			7102,
			commercedomain.ReferralPurchaseTypeMonthCard,
			"subscription_order",
			"trade-7102-duplicate",
		)
	})
	require.NoError(t, err)

	summary, err = GetUserSubscriptionResetOpportunity(7101)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.AvailableCount)
	assert.Equal(t, 1, summary.EarnedTotal)
}

func TestAwardReferralSubscriptionResetOpportunityTx_ExcludesCurrentSuccessfulOrder(t *testing.T) {
	db := setupRedemptionTestDB(t)

	insertSubscriptionResetAppTestUser(t, 7111, 0)
	insertSubscriptionResetAppTestUser(t, 7112, 7111)
	plan := insertSubscriptionResetAppTestPlan(t, 7112, 0, 1000)
	require.NoError(t, db.Create(&commerceschema.SubscriptionOrder{
		Id:      7112,
		UserId:  7112,
		PlanId:  plan.Id,
		Money:   50,
		TradeNo: "trade-7112-current",
		Status:  constant.TopUpStatusSuccess,
	}).Error)

	err := db.Transaction(func(tx *gorm.DB) error {
		return AwardReferralSubscriptionResetOpportunityTx(
			tx,
			7112,
			commercedomain.ReferralPurchaseTypeMonthCard,
			"subscription_order",
			"trade-7112-current",
		)
	})
	require.NoError(t, err)

	summary, err := GetUserSubscriptionResetOpportunity(7111)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.AvailableCount)
	assert.Equal(t, 1, summary.EarnedTotal)
}

func TestAwardReferralSubscriptionResetOpportunityTx_AllowsFirstMonthCardAfterDayPass(t *testing.T) {
	db := setupRedemptionTestDB(t)

	insertSubscriptionResetAppTestUser(t, 7131, 0)
	insertSubscriptionResetAppTestUser(t, 7132, 7131)
	dayPlan := insertSubscriptionResetAppTestPlan(t, 7132, 1, 100)
	monthPlan := insertSubscriptionResetAppTestPlan(t, 7133, 0, 1000)
	weeklyPlan := insertSubscriptionResetAppTestPlan(t, 7134, 7, 500)
	require.NoError(t, db.Create(&commerceschema.SubscriptionOrder{
		Id:      7132,
		UserId:  7132,
		PlanId:  dayPlan.Id,
		Money:   10,
		TradeNo: "trade-7132-day-pass",
		Status:  constant.TopUpStatusSuccess,
	}).Error)
	require.NoError(t, db.Create(&commerceschema.SubscriptionOrder{
		Id:      7134,
		UserId:  7132,
		PlanId:  weeklyPlan.Id,
		Money:   30,
		TradeNo: "trade-7132-week-card",
		Status:  constant.TopUpStatusSuccess,
	}).Error)
	require.NoError(t, db.Create(&commerceschema.SubscriptionOrder{
		Id:      7133,
		UserId:  7132,
		PlanId:  monthPlan.Id,
		Money:   50,
		TradeNo: "trade-7132-month-card",
		Status:  constant.TopUpStatusSuccess,
	}).Error)

	err := db.Transaction(func(tx *gorm.DB) error {
		return AwardReferralSubscriptionResetOpportunityTx(
			tx,
			7132,
			commercedomain.ReferralPurchaseTypeMonthCard,
			"subscription_order",
			"trade-7132-month-card",
		)
	})
	require.NoError(t, err)

	summary, err := GetUserSubscriptionResetOpportunity(7131)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.AvailableCount)
}

func TestAwardReferralSubscriptionResetOpportunityTx_RejectsLaterMonthCardPurchases(t *testing.T) {
	db := setupRedemptionTestDB(t)

	insertSubscriptionResetAppTestUser(t, 7141, 0)
	insertSubscriptionResetAppTestUser(t, 7142, 7141)
	monthPlan := insertSubscriptionResetAppTestPlan(t, 7142, 0, 1000)
	require.NoError(t, db.Create(&commerceschema.SubscriptionOrder{
		Id:      7142,
		UserId:  7142,
		PlanId:  monthPlan.Id,
		Money:   50,
		TradeNo: "trade-7142-first-month-card",
		Status:  constant.TopUpStatusSuccess,
	}).Error)
	require.NoError(t, db.Create(&commerceschema.SubscriptionOrder{
		Id:      7143,
		UserId:  7142,
		PlanId:  monthPlan.Id,
		Money:   50,
		TradeNo: "trade-7142-second-month-card",
		Status:  constant.TopUpStatusSuccess,
	}).Error)

	err := db.Transaction(func(tx *gorm.DB) error {
		return AwardReferralSubscriptionResetOpportunityTx(
			tx,
			7142,
			commercedomain.ReferralPurchaseTypeMonthCard,
			"subscription_order",
			"trade-7142-second-month-card",
		)
	})
	require.NoError(t, err)

	summary, err := GetUserSubscriptionResetOpportunity(7141)
	require.NoError(t, err)
	assert.Equal(t, 0, summary.AvailableCount)
}

func TestUseUserSubscriptionResetOpportunity_ClearsCurrentSubscriptionAndLimitsMonthlyUsage(t *testing.T) {
	db := setupRedemptionTestDB(t)

	insertSubscriptionResetAppTestUser(t, 7201, 0)
	planA := insertSubscriptionResetAppTestPlan(t, 7201, 0, 1000)
	planB := insertSubscriptionResetAppTestPlan(t, 7202, 0, 2000)

	now := time.Now().Unix()
	require.NoError(t, db.Create(&commerceschema.UserSubscription{
		Id:          7301,
		UserId:      7201,
		PlanId:      planA.Id,
		AmountTotal: planA.TotalAmount,
		AmountUsed:  360,
		StartTime:   now - 3600,
		EndTime:     now + 10*86400,
		Status:      "active",
	}).Error)
	require.NoError(t, db.Create(&commerceschema.UserSubscription{
		Id:          7302,
		UserId:      7201,
		PlanId:      planB.Id,
		AmountTotal: planB.TotalAmount,
		AmountUsed:  180,
		StartTime:   now - 3600,
		EndTime:     now + 20*86400,
		Status:      "active",
	}).Error)
	require.NoError(t, db.Create(&commerceschema.SubscriptionResetOpportunityAccount{
		UserId:         7201,
		EarnedTotal:    2,
		AvailableTotal: 2,
	}).Error)
	account, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{
		AccountType: "subscription",
		OwnerType:   "user_subscription",
		OwnerID:     7302,
		QuotaUnit:   "quota",
	})
	require.NoError(t, err)
	_, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{
		AccountID:      account.AccountID,
		Amount:         1820,
		IdempotencyKey: "subscription-reset-test-initial-balance",
		ReasonCode:     "test",
	})
	require.NoError(t, err)

	result, err := UseUserSubscriptionResetOpportunity(7201)
	require.NoError(t, err)
	assert.Equal(t, 7302, result.UserSubscriptionId)
	assert.EqualValues(t, 180, result.AmountUsedBefore)
	assert.EqualValues(t, 0, result.AmountUsedAfter)
	assert.Equal(t, 1, result.ResetOpportunity.AvailableCount)
	assert.Equal(t, 1, result.ResetOpportunity.UsedTotal)
	assert.True(t, result.ResetOpportunity.UsedThisMonth)

	var current commerceschema.UserSubscription
	require.NoError(t, db.Where("id = ?", 7302).First(&current).Error)
	assert.Zero(t, current.AmountUsed)
	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.Where("account_id = ?", account.AccountID).First(&snapshot).Error)
	assert.EqualValues(t, 2000, snapshot.AvailableBalance)

	var untouched commerceschema.UserSubscription
	require.NoError(t, db.Where("id = ?", 7301).First(&untouched).Error)
	assert.EqualValues(t, 360, untouched.AmountUsed)

	_, err = UseUserSubscriptionResetOpportunity(7201)
	require.ErrorIs(t, err, commerceschema.ErrSubscriptionResetOpportunityMonthlyUsed)

	summary, err := GetUserSubscriptionResetOpportunity(7201)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.AvailableCount)
	assert.Equal(t, 1, summary.UsedTotal)
}
