package app

import (
	"testing"
	"time"

	"github.com/sh2001sh/new-api/constant"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestConvertMonthlyPassToUnifiedCreditPartialPercentage(t *testing.T) {
	db, user, _, sub := setupMonthlyPassConversionTest(t, 9201, 9301, 9401, 49, 20)

	result, err := ConvertMonthlyPassToUnifiedCredit("req-partial-60", user.Id, sub.Id, 60)
	require.NoError(t, err)

	expectedQuota := int(decimal.NewFromInt(49).
		Mul(decimal.NewFromFloat(0.6)).
		Mul(decimal.NewFromFloat(platformruntime.QuotaPerUnit)).
		Floor().IntPart())
	assert.Equal(t, int64(platformruntime.QuotaPerUnit)*60, result.SourceQuota)
	assert.Equal(t, expectedQuota, result.TargetQuota)
	assert.Equal(t, 60, result.ConversionPercent)
	assert.InDelta(t, 0.8, result.UnusedRatio, 0.00000001)
	assert.InDelta(t, 0.2, result.RemainingRatioAfter, 0.00000001)
	assert.False(t, result.SubscriptionEnded)
	assert.Equal(t, 100+expectedQuota, result.QuotaAfter)
	assert.Equal(t, 60, result.Conversion.ConversionPercent)

	var savedUser identityschema.User
	require.NoError(t, db.Where("id = ?", user.Id).First(&savedUser).Error)
	assert.Equal(t, 100+expectedQuota, savedUser.ClaudeQuota)

	var savedSub commerceschema.UserSubscription
	require.NoError(t, db.Where("id = ?", sub.Id).First(&savedSub).Error)
	assert.Equal(t, int64(platformruntime.QuotaPerUnit)*80, savedSub.AmountUsed)
	assert.Equal(t, int64(platformruntime.QuotaPerUnit)*5, savedSub.PeriodUsed)
	assert.Equal(t, "active", savedSub.Status)
	assert.Equal(t, sub.EndTime, savedSub.EndTime)

	var account billingschema.BillingAccount
	require.NoError(t, db.Where("owner_type = ? AND owner_id = ? AND account_type = ?", "user_subscription", sub.Id, "subscription").First(&account).Error)
	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.Where("account_id = ?", account.AccountID).First(&snapshot).Error)
	assert.Equal(t, int64(platformruntime.QuotaPerUnit)*20, snapshot.AvailableBalance)
}

func TestConvertMonthlyPassToUnifiedCreditAllRemainingEndsPass(t *testing.T) {
	db, user, _, sub := setupMonthlyPassConversionTest(t, 9202, 9302, 9402, 49, 20)

	result, err := ConvertMonthlyPassToUnifiedCredit("req-full-80", user.Id, sub.Id, 80)
	require.NoError(t, err)

	expectedQuota := int(decimal.NewFromInt(49).
		Mul(decimal.NewFromFloat(0.8)).
		Mul(decimal.NewFromFloat(platformruntime.QuotaPerUnit)).
		Floor().IntPart())
	assert.Equal(t, expectedQuota, result.TargetQuota)
	assert.True(t, result.SubscriptionEnded)
	assert.Zero(t, result.RemainingRatioAfter)
	assert.Equal(t, 100+expectedQuota, result.QuotaAfter)

	var savedSub commerceschema.UserSubscription
	require.NoError(t, db.Where("id = ?", sub.Id).First(&savedSub).Error)
	assert.Equal(t, savedSub.AmountTotal, savedSub.AmountUsed)
	assert.Equal(t, savedSub.PeriodAmount, savedSub.PeriodUsed)
	assert.Equal(t, "cancelled", savedSub.Status)
	assert.LessOrEqual(t, savedSub.EndTime, time.Now().Unix())
}

func TestConvertMonthlyPassToUnifiedCreditMaxPercentageConsumesFractionalRemainder(t *testing.T) {
	db, user, plan, sub := setupMonthlyPassConversionTest(t, 99208, 99308, 99408, 49, 20)
	residualUsage := int64(123)
	require.NoError(t, db.Model(sub).Update("amount_used", sub.AmountUsed+residualUsage).Error)
	remaining := sub.AmountTotal - sub.AmountUsed

	preview := BuildSubscriptionClaudeConversionPreview(plan, sub)
	require.True(t, preview.Eligible)
	require.Equal(t, 79, preview.MaxConversionPercent)

	result, err := ConvertMonthlyPassToUnifiedCredit("req-full-fractional", user.Id, sub.Id, preview.MaxConversionPercent)
	require.NoError(t, err)
	require.True(t, result.SubscriptionEnded)
	require.Equal(t, remaining, result.SourceQuota)
	require.Zero(t, result.RemainingRatioAfter)

	var savedSub commerceschema.UserSubscription
	require.NoError(t, db.Where("id = ?", sub.Id).First(&savedSub).Error)
	require.Equal(t, savedSub.AmountTotal, savedSub.AmountUsed)
	require.Equal(t, "cancelled", savedSub.Status)

	var account billingschema.BillingAccount
	require.NoError(t, db.Where("owner_type = ? AND owner_id = ? AND account_type = ?", "user_subscription", sub.Id, "subscription").First(&account).Error)
	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.Where("account_id = ?", account.AccountID).First(&snapshot).Error)
	require.Zero(t, snapshot.AvailableBalance)
}

func TestConvertMonthlyPassToUnifiedCreditAllowsAllBelowOnePercent(t *testing.T) {
	db, user, plan, sub := setupMonthlyPassConversionTest(t, 99209, 99309, 99409, 49, 0)
	remaining := int64(12345)
	require.NoError(t, db.Model(sub).Update("amount_used", sub.AmountTotal-remaining).Error)
	sub.AmountUsed = sub.AmountTotal - remaining

	preview := BuildSubscriptionClaudeConversionPreview(plan, sub)
	require.True(t, preview.Eligible)
	require.Equal(t, 1, preview.MaxConversionPercent)
	require.Greater(t, preview.PreviewQuota, 0)

	result, err := ConvertMonthlyPassToUnifiedCredit("req-full-sub-percent", user.Id, sub.Id, preview.MaxConversionPercent)
	require.NoError(t, err)
	require.True(t, result.SubscriptionEnded)
	require.Equal(t, remaining, result.SourceQuota)
	require.Zero(t, result.RemainingRatioAfter)
}

func TestConvertMonthlyPassToUnifiedCreditWritesOffConversionDustOnly(t *testing.T) {
	db, user, plan, sub := setupMonthlyPassConversionTest(t, 99210, 99310, 99410, 49, 0)
	require.NoError(t, db.Model(sub).Update("amount_used", sub.AmountTotal-1).Error)

	preview := BuildSubscriptionClaudeConversionPreview(plan, sub)
	require.True(t, preview.Eligible)
	require.Equal(t, int64(1), preview.RemainingQuota)
	require.Equal(t, 1, preview.PreviewQuota)

	result, err := ConvertMonthlyPassToUnifiedCredit("req-conversion-dust", user.Id, sub.Id, preview.MaxConversionPercent)
	require.NoError(t, err)
	require.True(t, result.SubscriptionEnded)
	require.Equal(t, int64(1), result.SourceQuota)
	require.Equal(t, 1, result.TargetQuota)
	require.Equal(t, 101, result.QuotaAfter)

	var savedSub commerceschema.UserSubscription
	require.NoError(t, db.Where("id = ?", sub.Id).First(&savedSub).Error)
	require.Equal(t, savedSub.AmountTotal, savedSub.AmountUsed)
	require.Equal(t, "cancelled", savedSub.Status)
}

func TestConvertMonthlyPassToUnifiedCreditRecoversInterruptedReservation(t *testing.T) {
	db, user, _, sub := setupMonthlyPassConversionTest(t, 9207, 9307, 9407, 49, 20)
	account, err := billingdomain.EnsureBillingAccountTx(db, billingdomain.EnsureAccountParams{
		AccountType: "subscription", OwnerType: "user_subscription", OwnerID: int64(sub.Id), QuotaUnit: "quota",
	})
	require.NoError(t, err)
	seedAmount := int64(platformruntime.QuotaPerUnit) * 80
	_, err = billingdomain.CreditAccountTx(db, billingdomain.CreditAccountParams{
		AccountID: account.AccountID, Amount: seedAmount, IdempotencyKey: "seed-stale-conversion-balance",
		ReasonCode: "test_seed",
	})
	require.NoError(t, err)
	stale, err := billingdomain.CreateReservationTx(db, billingdomain.CreateReservationParams{
		AccountID: account.AccountID, RequestID: "stale-conversion", ReservedAmount: 10,
		IdempotencyKey: "monthly-pass-conversion:stale-conversion:reserve",
	})
	require.NoError(t, err)

	_, err = ConvertMonthlyPassToUnifiedCredit("req-after-interruption", user.Id, sub.Id, 60)
	require.NoError(t, err)

	var recovered billingschema.BillingReservation
	require.NoError(t, db.Where("reservation_id = ?", stale.ReservationID).First(&recovered).Error)
	assert.Equal(t, billingschema.BillingReservationStatusReleased, recovered.Status)
}

func TestConvertMonthlyPassToUnifiedCreditRejectsInvalidPercentages(t *testing.T) {
	_, user, _, sub := setupMonthlyPassConversionTest(t, 9203, 9303, 9403, 49, 20)

	tests := []struct {
		name    string
		percent int
	}{
		{name: "zero", percent: 0},
		{name: "negative", percent: -1},
		{name: "above remaining", percent: 81},
		{name: "above one hundred", percent: 101},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ConvertMonthlyPassToUnifiedCredit("req-invalid-"+tc.name, user.Id, sub.Id, tc.percent)
			require.Error(t, err)
		})
	}
}

func TestConvertMonthlyPassToUnifiedCreditRejectsDayPass(t *testing.T) {
	db := setupRedemptionTestDB(t)
	restoreSubscriptionClaudeConversionConfigForTest(t)
	commerceschema.SubscriptionClaudeConversionEnabled = true

	user := &identityschema.User{Id: 9204, Username: "day_pass_conversion", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)
	plan := &commerceschema.SubscriptionPlan{
		Id: 9304, Title: "50刀日卡", PlanType: commerceschema.SubscriptionPlanTypeDaily,
		PriceAmount: 50, DurationUnit: commerceschema.SubscriptionDurationDay,
		DurationValue: 1, TotalAmount: int64(platformruntime.QuotaPerUnit),
	}
	require.NoError(t, db.Create(plan).Error)
	sub := &commerceschema.UserSubscription{
		Id: 9404, UserId: user.Id, PlanId: plan.Id, AmountTotal: plan.TotalAmount,
		StartTime: time.Now().Add(-2 * time.Hour).Unix(), EndTime: time.Now().Add(12 * time.Hour).Unix(), Status: "active",
	}
	require.NoError(t, db.Create(sub).Error)

	_, err := ConvertMonthlyPassToUnifiedCredit("req-day-pass", user.Id, sub.Id, 50)
	require.ErrorIs(t, err, commerceschema.ErrSubscriptionClaudeConversionNoTarget)
}

func TestConvertMonthlyPassToUnifiedCreditIdempotencyIncludesPercentage(t *testing.T) {
	_, user, _, sub := setupMonthlyPassConversionTest(t, 9205, 9305, 9405, 169, 0)

	first, err := ConvertMonthlyPassToUnifiedCredit("req-idempotent-1", user.Id, sub.Id, 60)
	require.NoError(t, err)
	second, err := ConvertMonthlyPassToUnifiedCredit("req-idempotent-1", user.Id, sub.Id, 60)
	require.NoError(t, err)
	assert.Equal(t, first.TargetQuota, second.TargetQuota)
	assert.Equal(t, first.AmountUsedAfter, second.AmountUsedAfter)
	assert.Equal(t, first.QuotaAfter, second.QuotaAfter)

	_, err = ConvertMonthlyPassToUnifiedCredit("req-idempotent-1", user.Id, sub.Id, 50)
	require.ErrorIs(t, err, commerceschema.ErrSubscriptionClaudeConversionInvalid)
}

func TestConvertMonthlyPassToUnifiedCreditRejectsResetSubscription(t *testing.T) {
	db, user, _, sub := setupMonthlyPassConversionTest(t, 9206, 9306, 9406, 49, 20)
	require.NoError(t, db.Create(&commerceschema.SubscriptionResetOpportunityLedger{
		UserId:        user.Id,
		RelatedUserId: sub.Id,
		ChangeType:    commerceschema.SubscriptionResetOpportunityChangeUse,
		Delta:         -1,
		EventKey:      "monthly-pass-reset-before-conversion",
	}).Error)

	_, err := ConvertMonthlyPassToUnifiedCredit("req-after-reset", user.Id, sub.Id, 60)
	require.ErrorIs(t, err, commerceschema.ErrSubscriptionClaudeConversionResetUsed)
}

func setupMonthlyPassConversionTest(t *testing.T, userID, planID, subID int, price float64, usedPercent int) (*gorm.DB, *identityschema.User, *commerceschema.SubscriptionPlan, *commerceschema.UserSubscription) {
	t.Helper()
	db := setupRedemptionTestDB(t)
	restoreSubscriptionClaudeConversionConfigForTest(t)
	commerceschema.SubscriptionClaudeConversionEnabled = true
	user := &identityschema.User{Id: userID, Username: "monthly_pass_conversion", Status: constant.UserStatusEnabled, ClaudeQuota: 100}
	require.NoError(t, db.Create(user).Error)
	plan := &commerceschema.SubscriptionPlan{
		Id: planID, Title: "Lite月卡", PlanType: commerceschema.SubscriptionPlanTypeMonthly,
		PriceAmount: price, DurationUnit: commerceschema.SubscriptionDurationMonth, DurationValue: 1,
		TotalAmount:      int64(platformruntime.QuotaPerUnit) * 100,
		PeriodAmount:     int64(platformruntime.QuotaPerUnit) * 30,
		QuotaResetPeriod: commerceschema.SubscriptionResetNever,
	}
	require.NoError(t, db.Create(plan).Error)
	now := time.Now().Unix()
	sub := &commerceschema.UserSubscription{
		Id: subID, UserId: user.Id, PlanId: plan.Id, AmountTotal: plan.TotalAmount,
		AmountUsed:   int64(platformruntime.QuotaPerUnit) * int64(usedPercent),
		PeriodAmount: plan.PeriodAmount, PeriodUsed: int64(platformruntime.QuotaPerUnit) * 5,
		StartTime: now - 86400, EndTime: now + 86400, Status: "active",
	}
	require.NoError(t, db.Create(sub).Error)
	return db, user, plan, sub
}

func restoreSubscriptionClaudeConversionConfigForTest(t *testing.T) {
	t.Helper()
	enabled := commerceschema.SubscriptionClaudeConversionEnabled
	t.Cleanup(func() { commerceschema.SubscriptionClaudeConversionEnabled = enabled })
}
