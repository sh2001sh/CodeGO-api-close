package app

import (
	"testing"
	"time"

	"github.com/sh2001sh/new-api/constant"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertMonthlyPassToUnifiedCreditUsesUnusedPricePercentage(t *testing.T) {
	db := setupRedemptionTestDB(t)
	restoreSubscriptionClaudeConversionConfigForTest(t)
	commerceschema.SubscriptionClaudeConversionEnabled = true

	user := &identityschema.User{
		Id:          9201,
		Username:    "monthly_pass_conversion_success",
		Status:      constant.UserStatusEnabled,
		ClaudeQuota: 100,
	}
	require.NoError(t, db.Create(user).Error)

	plan := &commerceschema.SubscriptionPlan{
		Id:               9301,
		Title:            "Standard月卡",
		PlanType:         commerceschema.SubscriptionPlanTypeMonthly,
		PriceAmount:      89,
		DurationUnit:     commerceschema.SubscriptionDurationMonth,
		DurationValue:    1,
		TotalAmount:      int64(platformruntime.QuotaPerUnit * 3),
		PeriodAmount:     int64(platformruntime.QuotaPerUnit),
		QuotaResetPeriod: commerceschema.SubscriptionResetNever,
	}
	require.NoError(t, db.Create(plan).Error)

	now := time.Now().Unix()
	sub := &commerceschema.UserSubscription{
		Id:           9401,
		UserId:       user.Id,
		PlanId:       plan.Id,
		AmountTotal:  int64(platformruntime.QuotaPerUnit * 3),
		AmountUsed:   int64(platformruntime.QuotaPerUnit),
		PeriodAmount: int64(platformruntime.QuotaPerUnit),
		PeriodUsed:   int64(platformruntime.QuotaPerUnit / 3),
		StartTime:    now - 86400,
		EndTime:      now + 86400,
		Status:       "active",
	}
	require.NoError(t, db.Create(sub).Error)

	result, err := ConvertMonthlyPassToUnifiedCredit("req-success-1", user.Id, sub.Id)
	require.NoError(t, err)
	require.NotNil(t, result)

	expectedQuota := int(decimal.NewFromInt(89).
		Mul(decimal.NewFromInt(2)).
		Div(decimal.NewFromInt(3)).
		Mul(decimal.NewFromFloat(platformruntime.QuotaPerUnit)).
		Floor().IntPart())
	assert.Equal(t, int64(platformruntime.QuotaPerUnit*2), result.SourceQuota)
	assert.Equal(t, expectedQuota, result.TargetQuota)
	assert.InDelta(t, 2.0/3.0, result.UnusedRatio, 0.00000001)
	assert.Equal(t, 89.0, result.PlanPriceAmount)
	assert.Equal(t, 100+expectedQuota, result.QuotaAfter)

	var savedUser identityschema.User
	require.NoError(t, db.Where("id = ?", user.Id).First(&savedUser).Error)
	assert.Equal(t, 100+expectedQuota, savedUser.ClaudeQuota)

	var savedSub commerceschema.UserSubscription
	require.NoError(t, db.Where("id = ?", sub.Id).First(&savedSub).Error)
	assert.Equal(t, savedSub.AmountTotal, savedSub.AmountUsed)
	assert.Equal(t, savedSub.PeriodAmount, savedSub.PeriodUsed)
	assert.Equal(t, "cancelled", savedSub.Status)
	assert.LessOrEqual(t, savedSub.EndTime, time.Now().Unix())
}

func TestConvertMonthlyPassToUnifiedCreditRejectsDayPass(t *testing.T) {
	db := setupRedemptionTestDB(t)
	restoreSubscriptionClaudeConversionConfigForTest(t)
	commerceschema.SubscriptionClaudeConversionEnabled = true

	user := &identityschema.User{Id: 9202, Username: "day_pass_conversion", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)
	plan := &commerceschema.SubscriptionPlan{
		Id:            9302,
		Title:         "50刀日卡",
		PlanType:      commerceschema.SubscriptionPlanTypeDaily,
		PriceAmount:   50,
		DurationUnit:  commerceschema.SubscriptionDurationDay,
		DurationValue: 1,
		TotalAmount:   int64(platformruntime.QuotaPerUnit),
	}
	require.NoError(t, db.Create(plan).Error)
	sub := &commerceschema.UserSubscription{
		Id:          9402,
		UserId:      user.Id,
		PlanId:      plan.Id,
		AmountTotal: plan.TotalAmount,
		StartTime:   time.Now().Add(-2 * time.Hour).Unix(),
		EndTime:     time.Now().Add(12 * time.Hour).Unix(),
		Status:      "active",
	}
	require.NoError(t, db.Create(sub).Error)

	_, err := ConvertMonthlyPassToUnifiedCredit("req-day-pass", user.Id, sub.Id)
	require.ErrorIs(t, err, commerceschema.ErrSubscriptionClaudeConversionNoTarget)
}

func TestConvertMonthlyPassToUnifiedCreditIsIdempotent(t *testing.T) {
	db := setupRedemptionTestDB(t)
	restoreSubscriptionClaudeConversionConfigForTest(t)
	commerceschema.SubscriptionClaudeConversionEnabled = true

	user := &identityschema.User{Id: 9203, Username: "monthly_pass_idempotent", Status: constant.UserStatusEnabled, ClaudeQuota: 50}
	require.NoError(t, db.Create(user).Error)
	plan := &commerceschema.SubscriptionPlan{
		Id:            9303,
		Title:         "Pro月卡",
		PlanType:      commerceschema.SubscriptionPlanTypeMonthly,
		PriceAmount:   169,
		DurationUnit:  commerceschema.SubscriptionDurationMonth,
		DurationValue: 1,
		TotalAmount:   int64(platformruntime.QuotaPerUnit * 3),
	}
	require.NoError(t, db.Create(plan).Error)
	sub := &commerceschema.UserSubscription{
		Id:          9403,
		UserId:      user.Id,
		PlanId:      plan.Id,
		AmountTotal: plan.TotalAmount,
		StartTime:   time.Now().Add(-24 * time.Hour).Unix(),
		EndTime:     time.Now().Add(24 * time.Hour).Unix(),
		Status:      "active",
	}
	require.NoError(t, db.Create(sub).Error)

	first, err := ConvertMonthlyPassToUnifiedCredit("req-idempotent-1", user.Id, sub.Id)
	require.NoError(t, err)
	second, err := ConvertMonthlyPassToUnifiedCredit("req-idempotent-1", user.Id, sub.Id)
	require.NoError(t, err)

	assert.Equal(t, first.TargetQuota, second.TargetQuota)
	assert.Equal(t, first.AmountUsedAfter, second.AmountUsedAfter)
	assert.Equal(t, first.QuotaAfter, second.QuotaAfter)
	var count int64
	require.NoError(t, db.Model(&commerceschema.SubscriptionClaudeConversion{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func restoreSubscriptionClaudeConversionConfigForTest(t *testing.T) {
	t.Helper()
	enabled := commerceschema.SubscriptionClaudeConversionEnabled
	t.Cleanup(func() { commerceschema.SubscriptionClaudeConversionEnabled = enabled })
}
