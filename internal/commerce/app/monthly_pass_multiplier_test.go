package app

import (
	"testing"

	"github.com/sh2001sh/new-api/constant"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackfillMonthlyPassDoesNotRegrantExpiredReference(t *testing.T) {
	db := setupRedemptionTestDB(t)
	now := platformruntime.GetTimestamp()
	user := &identityschema.User{Id: 8830, Username: "monthly_pass_expired_idempotency", Status: constant.UserStatusEnabled}
	plan := &commerceschema.SubscriptionPlan{
		Id: 8830, Title: "Pro monthly expired idempotency", PlanType: commerceschema.SubscriptionPlanTypeMonthly,
		MembershipTier: commerceschema.SubscriptionMembershipTierPro,
	}
	sub := &commerceschema.UserSubscription{
		Id: 8830, UserId: user.Id, PlanId: plan.Id, Status: "active", StartTime: now - 60, EndTime: now + 86400,
	}
	require.NoError(t, db.Create(user).Error)
	require.NoError(t, db.Create(plan).Error)
	require.NoError(t, db.Create(sub).Error)
	require.NoError(t, BackfillActiveMonthlyPassBenefits())

	var original commerceschema.BlindBoxProp
	require.NoError(t, db.Where("user_id = ? AND prop_type = ?", user.Id, commerceschema.BlindBoxPropTypeMonthlyPassMultiplier).First(&original).Error)
	require.NoError(t, db.Model(&original).Updates(map[string]any{
		"status": commerceschema.BlindBoxPropStatusExpired, "expires_at": now - 1, "remaining_seconds": int64(0),
	}).Error)

	require.NoError(t, BackfillActiveMonthlyPassBenefits())
	var props []commerceschema.BlindBoxProp
	require.NoError(t, db.Where("user_id = ? AND prop_type = ?", user.Id, commerceschema.BlindBoxPropTypeMonthlyPassMultiplier).Find(&props).Error)
	require.Len(t, props, 1)
	assert.Equal(t, original.Id, props[0].Id)
	assert.Equal(t, commerceschema.BlindBoxPropStatusExpired, props[0].Status)
	assert.Equal(t, "monthly-pass-backfill-20260811:8830", props[0].BenefitReference)
}

func TestBackfillMonthlyPassSkipsSubscriptionWithOrderBenefit(t *testing.T) {
	db := setupRedemptionTestDB(t)
	now := platformruntime.GetTimestamp()
	user := &identityschema.User{Id: 8831, Username: "monthly_pass_order_backfill", Status: constant.UserStatusEnabled}
	plan := &commerceschema.SubscriptionPlan{
		Id: 8831, Title: "Standard monthly order benefit", PlanType: commerceschema.SubscriptionPlanTypeMonthly,
		MembershipTier: commerceschema.SubscriptionMembershipTierStandard,
	}
	sub := &commerceschema.UserSubscription{
		Id: 8831, UserId: user.Id, PlanId: plan.Id, Status: "active", StartTime: now - 60, EndTime: now + 86400,
	}
	order := &commerceschema.SubscriptionOrder{
		Id: 8831, UserId: user.Id, PlanId: plan.Id, TargetSubscriptionId: sub.Id,
		TradeNo: "monthly-pass-order-backfill-8831", Status: constant.TopUpStatusSuccess,
		FulfillmentStatus: commerceschema.SubscriptionOrderFulfillmentCompleted,
	}
	card := &commerceschema.BlindBoxProp{
		UserId: user.Id, PropType: commerceschema.BlindBoxPropTypeMonthlyPassMultiplier,
		Status: commerceschema.BlindBoxPropStatusExpired, Multiplier: 0.1,
		DurationSeconds: 30 * 60, BenefitReference: "monthly-pass-order:8831",
	}
	require.NoError(t, db.Create(user).Error)
	require.NoError(t, db.Create(plan).Error)
	require.NoError(t, db.Create(sub).Error)
	require.NoError(t, db.Create(order).Error)
	require.NoError(t, db.Create(card).Error)

	require.NoError(t, BackfillActiveMonthlyPassBenefits())
	var props []commerceschema.BlindBoxProp
	require.NoError(t, db.Where("user_id = ? AND prop_type = ?", user.Id, commerceschema.BlindBoxPropTypeMonthlyPassMultiplier).Find(&props).Error)
	require.Len(t, props, 1)
	assert.Equal(t, "monthly-pass-order:8831", props[0].BenefitReference)
}

func TestEnsureDefaultSubscriptionPlansDoesNotRunMonthlyPassBackfill(t *testing.T) {
	db := setupRedemptionTestDB(t)
	ensureSubscriptionSeedTestSchema(t)
	now := platformruntime.GetTimestamp()
	plan := requirePresetPlanByTitle(t, "Standard月卡")
	plan.Id = 8832
	insertSubscriptionStoreTestUser(t, 8832, nil)
	require.NoError(t, db.Create(&plan).Error)
	require.NoError(t, db.Create(&commerceschema.UserSubscription{
		Id: 8832, UserId: 8832, PlanId: plan.Id, Status: "active", StartTime: now - 60, EndTime: now + 86400,
		AmountTotal: plan.TotalAmount,
	}).Error)

	require.NoError(t, EnsureDefaultSubscriptionPlans())
	var count int64
	require.NoError(t, db.Model(&commerceschema.BlindBoxProp{}).
		Where("user_id = ? AND prop_type = ?", 8832, commerceschema.BlindBoxPropTypeMonthlyPassMultiplier).
		Count(&count).Error)
	assert.Zero(t, count)
}

func TestMonthlyPassEntitlementBindsExactCardAndOriginalExpiry(t *testing.T) {
	db := setupRedemptionTestDB(t)
	now := platformruntime.GetTimestamp()
	user := &identityschema.User{Id: 8833, Username: "monthly_pass_entitlement", Status: constant.UserStatusEnabled}
	card := &commerceschema.BlindBoxProp{
		UserId: user.Id, PropType: commerceschema.BlindBoxPropTypeMonthlyPassMultiplier,
		Status: commerceschema.BlindBoxPropStatusActive, Multiplier: 0.1,
		DurationSeconds: 30 * 60, RemainingSeconds: 30 * 60,
		ActivatedAt: now, ExpiresAt: now + 600, BenefitReference: "monthly-pass-order:8833",
	}
	require.NoError(t, db.Create(user).Error)
	require.NoError(t, db.Create(card).Error)

	entitlement, err := ActiveMonthlyPassEntitlement(user.Id)
	require.NoError(t, err)
	require.NotNil(t, entitlement)
	assert.Equal(t, card.Id, entitlement.PropID)
	assert.Equal(t, card.ExpiresAt, entitlement.ExpiresAt)
	valid, err := ValidateMonthlyPassEntitlement(user.Id, *entitlement)
	require.NoError(t, err)
	assert.True(t, valid)
	requiresOfficial, monthlyPassActive := ActiveMultiplierCardRoutePolicy(user.Id)
	assert.True(t, requiresOfficial)
	assert.True(t, monthlyPassActive)

	invalidCases := map[string]MonthlyPassEntitlement{
		"different card":   {PropID: card.Id + 1, Multiplier: 0.1, ExpiresAt: card.ExpiresAt},
		"wrong multiplier": {PropID: card.Id, Multiplier: 0.2, ExpiresAt: card.ExpiresAt},
		"original expired": {PropID: card.Id, Multiplier: 0.1, ExpiresAt: now - 1},
	}
	for name, candidate := range invalidCases {
		t.Run(name, func(t *testing.T) {
			valid, validateErr := ValidateMonthlyPassEntitlement(user.Id, candidate)
			require.NoError(t, validateErr)
			assert.False(t, valid)
		})
	}

	require.NoError(t, db.Model(card).Update("expires_at", now+3600).Error)
	valid, err = ValidateMonthlyPassEntitlement(user.Id, *entitlement)
	require.NoError(t, err)
	assert.True(t, valid, "extending the card must not replace the request's bound expiry")
	assert.Equal(t, now+600, entitlement.ExpiresAt)

	require.NoError(t, db.Model(card).Update("status", commerceschema.BlindBoxPropStatusPaused).Error)
	valid, err = ValidateMonthlyPassEntitlement(user.Id, *entitlement)
	require.NoError(t, err)
	assert.False(t, valid)
}
