package app

import (
	"github.com/sh2001sh/new-api/constant"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"testing"
	"time"
)

func TestResolveSubscriptionPurchasePreview_UpgradeUsesFullTargetPrice(t *testing.T) {
	db := setupRedemptionTestDB(t)

	user := &identityschema.User{
		Id:       9210,
		Username: "upgrade_preview_user",
		Status:   constant.UserStatusEnabled,
	}
	require.NoError(t, db.Create(user).Error)

	currentPlan := &commerceschema.SubscriptionPlan{
		Id:            9310,
		Title:         "Lite月卡",
		DurationUnit:  commerceschema.SubscriptionDurationMonth,
		DurationValue: 1,
		PriceAmount:   50,
		TotalAmount:   int64(platformruntime.QuotaPerUnit * 3),
	}
	targetPlan := &commerceschema.SubscriptionPlan{
		Id:            9311,
		Title:         "Standard月卡",
		DurationUnit:  commerceschema.SubscriptionDurationMonth,
		DurationValue: 1,
		PriceAmount:   90,
		TotalAmount:   int64(platformruntime.QuotaPerUnit * 6),
	}
	require.NoError(t, db.Create(currentPlan).Error)
	require.NoError(t, db.Create(targetPlan).Error)

	now := time.Now().Unix()
	require.NoError(t, db.Create(&commerceschema.UserSubscription{
		Id:          9410,
		UserId:      user.Id,
		PlanId:      currentPlan.Id,
		AmountTotal: currentPlan.TotalAmount,
		AmountUsed:  int64(platformruntime.QuotaPerUnit),
		StartTime:   now - 29*24*3600,
		EndTime:     now + 2*3600,
		Status:      "active",
	}).Error)

	preview, err := ResolveSubscriptionPurchasePreview(user.Id, targetPlan)
	require.NoError(t, err)
	require.NotNil(t, preview)
	assert.Equal(t, commerceschema.SubscriptionPurchaseActionUpgrade, preview.Action)
	assert.InDelta(t, 56.67, preview.AmountDue, 0.01)
}

func TestResolveSubscriptionPurchasePreview_ResetQuotaDoesNotDiscountUpgrade(t *testing.T) {
	db := setupRedemptionTestDB(t)
	user := &identityschema.User{Id: 9213, Username: "reset_upgrade_preview", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)
	currentPlan := &commerceschema.SubscriptionPlan{Id: 9316, Title: "Lite月卡", DurationUnit: commerceschema.SubscriptionDurationMonth, DurationValue: 1, PriceAmount: 49, TotalAmount: quotaUnitsFromUSD(350)}
	targetPlan := &commerceschema.SubscriptionPlan{Id: 9317, Title: "Standard月卡", DurationUnit: commerceschema.SubscriptionDurationMonth, DurationValue: 1, PriceAmount: 89, TotalAmount: quotaUnitsFromUSD(650)}
	require.NoError(t, db.Create(currentPlan).Error)
	require.NoError(t, db.Create(targetPlan).Error)
	now := time.Now().Unix()
	sub := &commerceschema.UserSubscription{Id: 9413, UserId: user.Id, PlanId: currentPlan.Id, AmountTotal: currentPlan.TotalAmount, AmountUsed: 0, StartTime: now - 10*24*3600, EndTime: now + 20*24*3600, Status: "active"}
	require.NoError(t, db.Create(sub).Error)
	require.NoError(t, db.Create(&commerceschema.SubscriptionResetOpportunityLedger{UserId: user.Id, RelatedUserId: sub.Id, ChangeType: commerceschema.SubscriptionResetOpportunityChangeUse, Delta: -1, EventKey: "reset-upgrade-price"}).Error)

	preview, err := ResolveSubscriptionPurchasePreview(user.Id, targetPlan)
	require.NoError(t, err)
	assert.Equal(t, commerceschema.SubscriptionPurchaseActionUpgrade, preview.Action)
	assert.Equal(t, targetPlan.PriceAmount, preview.BaseAmountDue)
}

func TestResolveSubscriptionPurchasePreview_ResetQuotaDoesNotDiscountRenewal(t *testing.T) {
	db := setupRedemptionTestDB(t)
	user := &identityschema.User{Id: 9214, Username: "reset_renew_preview", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)
	plan := &commerceschema.SubscriptionPlan{Id: 9318, Title: "Lite月卡", DurationUnit: commerceschema.SubscriptionDurationMonth, DurationValue: 1, PriceAmount: 49, TotalAmount: quotaUnitsFromUSD(350)}
	require.NoError(t, db.Create(plan).Error)
	now := time.Now().Unix()
	sub := &commerceschema.UserSubscription{Id: 9414, UserId: user.Id, PlanId: plan.Id, AmountTotal: plan.TotalAmount, AmountUsed: quotaUnitsFromUSD(35), StartTime: now - 10*24*3600, EndTime: now + 20*24*3600, Status: "active"}
	require.NoError(t, db.Create(sub).Error)
	require.NoError(t, db.Create(&commerceschema.SubscriptionResetOpportunityLedger{UserId: user.Id, RelatedUserId: sub.Id, ChangeType: commerceschema.SubscriptionResetOpportunityChangeUse, Delta: -1, EventKey: "reset-renew-price"}).Error)

	preview, err := ResolveSubscriptionPurchasePreview(user.Id, plan)
	require.NoError(t, err)
	assert.Equal(t, commerceschema.SubscriptionPurchaseActionRenew, preview.Action)
	assert.Equal(t, plan.PriceAmount, preview.BaseAmountDue)
}

func TestResolveSubscriptionPurchasePreview_DepletedHigherTierAllowsLowerTier(t *testing.T) {
	db := setupRedemptionTestDB(t)

	user := &identityschema.User{
		Id:       9211,
		Username: "depleted_preview_user",
		Status:   constant.UserStatusEnabled,
	}
	require.NoError(t, db.Create(user).Error)

	currentPlan := &commerceschema.SubscriptionPlan{
		Id:            9312,
		Title:         "Standard月卡",
		DurationUnit:  commerceschema.SubscriptionDurationMonth,
		DurationValue: 1,
		PriceAmount:   90,
		TotalAmount:   int64(platformruntime.QuotaPerUnit * 6),
	}
	targetPlan := &commerceschema.SubscriptionPlan{
		Id:            9313,
		Title:         "标准周卡",
		DurationUnit:  commerceschema.SubscriptionDurationDay,
		DurationValue: 7,
		PriceAmount:   29,
		TotalAmount:   int64(platformruntime.QuotaPerUnit),
	}
	require.NoError(t, db.Create(currentPlan).Error)
	require.NoError(t, db.Create(targetPlan).Error)

	now := time.Now().Unix()
	require.NoError(t, db.Create(&commerceschema.UserSubscription{
		Id:          9411,
		UserId:      user.Id,
		PlanId:      currentPlan.Id,
		AmountTotal: currentPlan.TotalAmount,
		AmountUsed:  currentPlan.TotalAmount,
		StartTime:   now - 15*24*3600,
		EndTime:     now + 2*24*3600,
		Status:      "active",
	}).Error)

	preview, err := ResolveSubscriptionPurchasePreview(user.Id, targetPlan)
	require.NoError(t, err)
	require.NotNil(t, preview)
	assert.Equal(t, commerceschema.SubscriptionPurchaseActionSubscribe, preview.Action)
	assert.Equal(t, 29.0, preview.AmountDue)
	assert.Empty(t, preview.DisabledReason)
}

func TestResolveSubscriptionPurchasePreview_TinyRemainderAllowsLowerTier(t *testing.T) {
	db := setupRedemptionTestDB(t)

	user := &identityschema.User{
		Id:       9212,
		Username: "tiny_remainder_preview_user",
		Status:   constant.UserStatusEnabled,
	}
	require.NoError(t, db.Create(user).Error)

	currentPlan := &commerceschema.SubscriptionPlan{
		Id:            9314,
		Title:         "Standard月卡",
		DurationUnit:  commerceschema.SubscriptionDurationMonth,
		DurationValue: 1,
		PriceAmount:   90,
		TotalAmount:   quotaUnitsFromUSD(1200),
	}
	targetPlan := &commerceschema.SubscriptionPlan{
		Id:            9315,
		Title:         "标准周卡",
		DurationUnit:  commerceschema.SubscriptionDurationDay,
		DurationValue: 7,
		PriceAmount:   29,
		TotalAmount:   quotaUnitsFromUSD(220),
	}
	require.NoError(t, db.Create(currentPlan).Error)
	require.NoError(t, db.Create(targetPlan).Error)

	now := time.Now().Unix()
	require.NoError(t, db.Create(&commerceschema.UserSubscription{
		Id:          9412,
		UserId:      user.Id,
		PlanId:      currentPlan.Id,
		AmountTotal: currentPlan.TotalAmount,
		AmountUsed:  currentPlan.TotalAmount - quotaUnitsFromUSD(0.002),
		StartTime:   now - 15*24*3600,
		EndTime:     now + 2*24*3600,
		Status:      "active",
	}).Error)

	preview, err := ResolveSubscriptionPurchasePreview(user.Id, targetPlan)
	require.NoError(t, err)
	require.NotNil(t, preview)
	assert.Equal(t, commerceschema.SubscriptionPurchaseActionSubscribe, preview.Action)
	assert.Equal(t, 29.0, preview.AmountDue)
	assert.Empty(t, preview.DisabledReason)
}

func TestUpgradeUserSubscriptionWithPlanTx_ResetsUsageAndStartsNewCycle(t *testing.T) {
	db := setupRedemptionTestDB(t)

	user := &identityschema.User{
		Id:       9220,
		Username: "upgrade_apply_user",
		Status:   constant.UserStatusEnabled,
	}
	require.NoError(t, db.Create(user).Error)

	currentPlan := &commerceschema.SubscriptionPlan{
		Id:            9320,
		Title:         "Lite月卡",
		DurationUnit:  commerceschema.SubscriptionDurationMonth,
		DurationValue: 1,
		PriceAmount:   50,
		TotalAmount:   int64(platformruntime.QuotaPerUnit * 3),
	}
	targetPlan := &commerceschema.SubscriptionPlan{
		Id:            9321,
		Title:         "Standard月卡",
		DurationUnit:  commerceschema.SubscriptionDurationMonth,
		DurationValue: 1,
		PriceAmount:   90,
		TotalAmount:   int64(platformruntime.QuotaPerUnit * 6),
	}
	require.NoError(t, db.Create(currentPlan).Error)
	require.NoError(t, db.Create(targetPlan).Error)

	now := time.Now().Unix()
	sub := &commerceschema.UserSubscription{
		Id:           9420,
		UserId:       user.Id,
		PlanId:       currentPlan.Id,
		AmountTotal:  currentPlan.TotalAmount,
		AmountUsed:   int64(platformruntime.QuotaPerUnit),
		PeriodAmount: 0,
		PeriodUsed:   0,
		StartTime:    now - 10*24*3600,
		EndTime:      now + 2*3600,
		Status:       "active",
	}
	require.NoError(t, db.Create(sub).Error)

	var upgraded *commerceschema.UserSubscription
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		locked := &commerceschema.UserSubscription{}
		if err := tx.Where("id = ?", sub.Id).First(locked).Error; err != nil {
			return err
		}
		var err error
		upgraded, err = upgradeUserSubscriptionWithPlanTx(tx, locked, targetPlan, "order")
		return err
	}))
	require.NotNil(t, upgraded)
	assert.Equal(t, targetPlan.Id, upgraded.PlanId)
	assert.EqualValues(t, 0, upgraded.AmountUsed)
	assert.Equal(t, targetPlan.TotalAmount, upgraded.AmountTotal)
	assert.GreaterOrEqual(t, upgraded.StartTime, now)
	assert.Greater(t, upgraded.EndTime, upgraded.StartTime)
}
