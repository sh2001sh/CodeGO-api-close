package app

import (
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func requirePresetPlanByTitle(t *testing.T, title string) commerceschema.SubscriptionPlan {
	t.Helper()
	for _, plan := range defaultSubscriptionPlans() {
		if plan.Title == title {
			return plan
		}
	}
	t.Fatalf("preset plan not found: %s", title)
	return commerceschema.SubscriptionPlan{}
}

func ensureSubscriptionSeedTestSchema(t *testing.T) {
	t.Helper()
	require.NoError(t, platformdb.DB.AutoMigrate(&commerceschema.SubscriptionPreConsumeRecord{}))
	t.Cleanup(func() {
		platformdb.DB.Exec("DELETE FROM subscription_pre_consume_records")
	})
}

func pickFirstBillableSubscriptionID(t *testing.T, userID int, amount int64) int {
	t.Helper()

	now := platformruntime.GetTimestamp()
	var subs []commerceschema.UserSubscription
	err := platformdb.DB.Where("user_id = ? AND status = ? AND end_time > ?", userID, "active", now).
		Order("end_time desc, id desc").
		Find(&subs).Error
	require.NoError(t, err)

	for _, sub := range subs {
		sub := sub
		plan, err := getSubscriptionPlanRecordTx(nil, sub.PlanId)
		require.NoError(t, err)

		if sub.AmountTotal > 0 {
			remain := sub.AmountTotal - sub.AmountUsed
			if remain < amount {
				continue
			}
		}

		periodAmount := getSubscriptionPeriodAmount(plan, &sub)
		if !usesLegacySubscriptionPeriodicQuota(plan, &sub) && periodAmount > 0 {
			periodRemain := periodAmount - sub.PeriodUsed
			if periodRemain < amount {
				continue
			}
		}
		return sub.Id
	}
	return 0
}

func TestEnsureDefaultSubscriptionPlans_FixesLegacyPresetUsageUnits(t *testing.T) {
	db := setupRedemptionTestDB(t)
	ensureSubscriptionSeedTestSchema(t)

	now := time.Now().Unix()
	preset := requirePresetPlanByTitle(t, "50刀日卡")
	legacyPlan := preset
	legacyPlan.Id = 9101
	legacyPlan.TotalAmount = 50
	legacyPlan.PeriodAmount = 0
	require.NoError(t, db.Create(&legacyPlan).Error)

	insertSubscriptionStoreTestUser(t, 9101, []int{9201})
	legacySub := &commerceschema.UserSubscription{
		Id:          9201,
		UserId:      9101,
		PlanId:      legacyPlan.Id,
		AmountTotal: 50,
		AmountUsed:  50,
		StartTime:   now - 3600,
		EndTime:     now + 86400,
		Status:      "active",
	}
	require.NoError(t, db.Create(legacySub).Error)

	require.NoError(t, EnsureDefaultSubscriptionPlans())

	var reloadedSub commerceschema.UserSubscription
	require.NoError(t, db.Where("id = ?", legacySub.Id).First(&reloadedSub).Error)

	expectedQuota := quotaUnitsFromUSD(50)
	assert.Equal(t, expectedQuota, reloadedSub.AmountTotal)
	assert.Equal(t, expectedQuota, reloadedSub.AmountUsed)
	assert.Zero(t, pickFirstBillableSubscriptionID(t, 9101, int64(platformruntime.QuotaPerUnit)))
}

func TestDefaultDayPassesDoNotParticipateInCollectiveBenefit(t *testing.T) {
	for _, title := range []string{"50刀日卡", "100刀日卡"} {
		plan := requirePresetPlanByTitle(t, title)
		assert.False(t, plan.GroupBuyEnabled)
		assert.Zero(t, plan.GroupBuyBonus2)
		assert.Zero(t, plan.GroupBuyBonus3)
		assert.Zero(t, plan.GroupBuyBonus5)
	}
}

func TestEnsureDefaultSubscriptionPlans_RepairsCollapsedMonthlyQuotaSnapshot(t *testing.T) {
	db := setupRedemptionTestDB(t)
	ensureSubscriptionSeedTestSchema(t)

	now := time.Now().Unix()
	monthPlan := defaultSubscriptionPlans()[0]
	monthPlan.Id = 9501
	require.NoError(t, db.Create(&monthPlan).Error)

	insertSubscriptionStoreTestUser(t, 9501, []int{9601})
	collapsedSub := &commerceschema.UserSubscription{
		Id:            9601,
		UserId:        9501,
		PlanId:        monthPlan.Id,
		AmountTotal:   quotaUnitsFromUSD(50),
		AmountUsed:    quotaUnitsFromUSD(20),
		PeriodAmount:  quotaUnitsFromUSD(50),
		PeriodUsed:    quotaUnitsFromUSD(20),
		StartTime:     now - 3600,
		EndTime:       now + 30*86400,
		Status:        "active",
		LastResetTime: now - 3600,
		NextResetTime: now + 6*86400,
	}
	require.NoError(t, db.Create(collapsedSub).Error)
	account, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{
		AccountType: "subscription", OwnerType: "user_subscription", OwnerID: int64(collapsedSub.Id), QuotaUnit: "quota",
	})
	require.NoError(t, err)
	legacyAvailable := collapsedSub.AmountTotal - collapsedSub.AmountUsed
	_, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{
		AccountID: account.AccountID, Amount: legacyAvailable, IdempotencyKey: "legacy-subscription-credit",
		ReasonCode: "test", ReferenceType: "user_subscription", ReferenceID: "9601", OperatorType: "test", OperatorID: "seed",
	})
	require.NoError(t, err)

	require.NoError(t, EnsureDefaultSubscriptionPlans())

	var reloadedSub commerceschema.UserSubscription
	require.NoError(t, db.Where("id = ?", collapsedSub.Id).First(&reloadedSub).Error)
	assert.Equal(t, monthPlan.TotalAmount, reloadedSub.AmountTotal)
	assert.Equal(t, quotaUnitsFromUSD(20), reloadedSub.AmountUsed)
	assert.Equal(t, monthPlan.PeriodAmount, reloadedSub.PeriodAmount)
	assert.Zero(t, reloadedSub.PeriodUsed)
	assert.Zero(t, reloadedSub.LastResetTime)
	assert.Zero(t, reloadedSub.NextResetTime)

	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.Where("account_id = ?", account.AccountID).First(&snapshot).Error)
	assert.Equal(t, monthPlan.TotalAmount-reloadedSub.AmountUsed, snapshot.AvailableBalance)
}

func TestEnsureDefaultSubscriptionPlans_UpdatesLegacyGroupBuyColumnsWithoutMissingColumnError(t *testing.T) {
	db := setupRedemptionTestDB(t)
	ensureSubscriptionSeedTestSchema(t)

	preset := defaultSubscriptionPlans()[0]
	legacyPlan := preset
	legacyPlan.Id = 9701
	legacyPlan.GroupBuyBonus2 = 0
	legacyPlan.GroupBuyBonus3 = 0
	legacyPlan.GroupBuyBonus5 = 0
	require.NoError(t, db.Create(&legacyPlan).Error)

	require.NoError(t, EnsureDefaultSubscriptionPlans())

	var reloadedPlan commerceschema.SubscriptionPlan
	require.NoError(t, db.Where("id = ?", legacyPlan.Id).First(&reloadedPlan).Error)
	assert.Equal(t, preset.GroupBuyBonus2, reloadedPlan.GroupBuyBonus2)
	assert.Equal(t, preset.GroupBuyBonus3, reloadedPlan.GroupBuyBonus3)
	assert.Equal(t, preset.GroupBuyBonus5, reloadedPlan.GroupBuyBonus5)
}

func TestEnsureDefaultSubscriptionPlans_UpdatesFuelConfiguration(t *testing.T) {
	db := setupRedemptionTestDB(t)

	preset := requirePresetPlanByTitle(t, "Lite月卡")
	legacyPlan := preset
	legacyPlan.Id = 9801
	legacyPlan.FuelEnabled = false
	legacyPlan.FuelUnitPrice = 0
	legacyPlan.FuelMinQuota = 0
	legacyPlan.FuelQuotaStep = 0
	require.NoError(t, db.Create(&legacyPlan).Error)

	require.NoError(t, EnsureDefaultSubscriptionPlans())

	var reloadedPlan commerceschema.SubscriptionPlan
	require.NoError(t, db.Where("id = ?", legacyPlan.Id).First(&reloadedPlan).Error)
	assert.Equal(t, preset.FuelEnabled, reloadedPlan.FuelEnabled)
	assert.Equal(t, preset.FuelUnitPrice, reloadedPlan.FuelUnitPrice)
	assert.Equal(t, preset.FuelMinQuota, reloadedPlan.FuelMinQuota)
	assert.Equal(t, preset.FuelQuotaStep, reloadedPlan.FuelQuotaStep)
}
