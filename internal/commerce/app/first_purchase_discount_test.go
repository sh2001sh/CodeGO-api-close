package app

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commercestore "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestApplyFirstPurchaseDiscountTxReservesEligibilityOnce(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:first-purchase-discount?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&identityschema.User{},
		&commerceschema.TopUp{},
		&commerceschema.SubscriptionOrder{},
		&commerceschema.SubscriptionPlan{},
		&commerceschema.UserSubscription{},
	))

	originalDB := platformdb.DB
	platformdb.DB = db
	t.Cleanup(func() { platformdb.DB = originalDB })

	setting := commercestore.GetPaymentSetting()
	originalEnabled := setting.FirstPurchaseDiscountEnabled
	originalMultiplier := setting.FirstPurchaseDiscountMultiplier
	originalStartAt := setting.FirstPurchaseDiscountStartAt
	originalEndAt := setting.FirstPurchaseDiscountEndAt
	t.Cleanup(func() {
		setting.FirstPurchaseDiscountEnabled = originalEnabled
		setting.FirstPurchaseDiscountMultiplier = originalMultiplier
		setting.FirstPurchaseDiscountStartAt = originalStartAt
		setting.FirstPurchaseDiscountEndAt = originalEndAt
	})

	now := time.Now().UTC()
	setting.FirstPurchaseDiscountEnabled = true
	setting.FirstPurchaseDiscountMultiplier = 0.8
	setting.FirstPurchaseDiscountStartAt = now.Add(-time.Hour).Unix()
	setting.FirstPurchaseDiscountEndAt = now.Add(time.Hour).Unix()

	user := identityschema.User{Username: "first_purchase_user", Password: "password123"}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&commerceschema.TopUp{
		UserId:  user.Id,
		Money:   100,
		TradeNo: "wallet-topup-does-not-consume-plan-discount",
		Status:  "success",
	}).Error)

	plan := &commerceschema.SubscriptionPlan{
		Id:            1,
		Title:         "Lite月卡",
		PlanType:      commerceschema.SubscriptionPlanTypeMonthly,
		DurationUnit:  commerceschema.SubscriptionDurationMonth,
		DurationValue: 1,
		PriceAmount:   100,
	}
	require.NoError(t, db.Create(plan).Error)
	excludedPlans := []*commerceschema.SubscriptionPlan{
		{Id: 2, Title: "新人体验卡", PlanType: commerceschema.SubscriptionPlanTypeStarter, DurationUnit: commerceschema.SubscriptionDurationDay, DurationValue: 1, PriceAmount: 1.9},
		{Id: 3, Title: "50刀日卡", PlanType: commerceschema.SubscriptionPlanTypeDaily, DurationUnit: commerceschema.SubscriptionDurationDay, DurationValue: 1, PriceAmount: 8.9},
		{Id: 4, Title: "标准周卡", PlanType: commerceschema.SubscriptionPlanTypeWeekly, DurationUnit: commerceschema.SubscriptionDurationDay, DurationValue: 7, PriceAmount: 34.9},
	}
	for _, excludedPlan := range excludedPlans {
		require.NoError(t, db.Create(excludedPlan).Error)
		excludedPreview, err := ResolveSubscriptionPurchasePreview(user.Id, excludedPlan)
		require.NoError(t, err)
		require.False(t, excludedPreview.FirstPurchaseDiscountApplied)
		require.InDelta(t, excludedPlan.PriceAmount, excludedPreview.AmountDue, 0.001)
		require.NoError(t, db.Create(&commerceschema.SubscriptionOrder{
			UserId: user.Id, PlanId: excludedPlan.Id, Money: excludedPlan.PriceAmount,
			TradeNo: "excluded-first-purchase-" + excludedPlan.Title, Status: "success",
		}).Error)
	}
	firstPurchasePreview, err := ResolveSubscriptionPurchasePreview(user.Id, plan)
	require.NoError(t, err)
	require.True(t, firstPurchasePreview.FirstPurchaseDiscountApplied)
	require.InDelta(t, 80, firstPurchasePreview.AmountDue, 0.001)

	preview := &commercedomain.SubscriptionPurchasePreview{BaseAmountDue: 100, AmountDue: 100}
	require.NoError(t, applyFirstPurchaseDiscountPreview(db, user.Id, plan, preview, now))
	require.True(t, preview.FirstPurchaseDiscountApplied)
	require.InDelta(t, 80, preview.AmountDue, 0.001)

	first := commerceschema.SubscriptionOrder{
		UserId:       user.Id,
		PlanId:       1,
		Money:        100,
		TradeNo:      "first-discount-1",
		PurchaseType: commerceschema.SubscriptionPurchaseTypeNormal,
		Status:       "pending",
	}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		if err := applyFirstPurchaseDiscountTx(tx, &first, 100, now); err != nil {
			return err
		}
		return tx.Create(&first).Error
	}))
	require.True(t, first.FirstPurchaseDiscountApplied)
	require.InDelta(t, 80, first.Money, 0.001)

	second := commerceschema.SubscriptionOrder{
		UserId:       user.Id,
		PlanId:       plan.Id,
		Money:        100,
		TradeNo:      "first-discount-2",
		PurchaseType: commerceschema.SubscriptionPurchaseTypeNormal,
		Status:       "pending",
	}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		if err := applyFirstPurchaseDiscountTx(tx, &second, 100, now); err != nil {
			return err
		}
		return tx.Create(&second).Error
	}))
	require.False(t, second.FirstPurchaseDiscountApplied)
	require.InDelta(t, 100, second.Money, 0.001)

	reservedPreview := &commercedomain.SubscriptionPurchasePreview{BaseAmountDue: 100, AmountDue: 100}
	require.NoError(t, applyFirstPurchaseDiscountPreview(db, user.Id, plan, reservedPreview, now))
	require.False(t, reservedPreview.FirstPurchaseDiscountApplied)
	require.InDelta(t, 100, reservedPreview.AmountDue, 0.001)
}
