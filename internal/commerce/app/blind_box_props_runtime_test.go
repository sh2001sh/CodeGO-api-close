package app

import (
	"github.com/sh2001sh/new-api/constant"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"testing"
	"time"
)

func TestActivateBlindBoxProp_AppliesConsumptionDiscount(t *testing.T) {
	db := setupRedemptionTestDB(t)

	user := &identityschema.User{
		Id:       8810,
		Username: "blind_box_prop_activation_user",
		Status:   constant.UserStatusEnabled,
	}
	require.NoError(t, db.Create(user).Error)

	var created *commerceschema.BlindBoxProp
	err := db.Transaction(func(tx *gorm.DB) error {
		var txErr error
		created, txErr = createBlindBoxPropTx(tx, user.Id, 1, "0.9 倍率卡")
		return txErr
	})
	require.NoError(t, err)
	require.NotNil(t, created)

	assert.Equal(t, 0.0, GetUserBlindBoxConsumptionDiscountRate(user.Id))

	activated, err := ActivateBlindBoxProp(user.Id, created.Id)
	require.NoError(t, err)
	require.NotNil(t, activated)
	assert.Equal(t, commerceschema.BlindBoxPropStatusActive, activated.Status)
	assert.NotZero(t, activated.ActivatedAt)
	assert.Greater(t, activated.ExpiresAt, activated.ActivatedAt)
	assert.InDelta(t, 0.10, activated.DiscountRate, 0.0001)
	assert.InDelta(t, 0.10, GetUserBlindBoxConsumptionDiscountRate(user.Id), 0.0001)

	props, err := ListUserBlindBoxProps(user.Id)
	require.NoError(t, err)
	require.Len(t, props, 1)
	assert.Equal(t, commerceschema.BlindBoxPropStatusActive, props[0].Status)
}

func TestListUserBlindBoxPropsRepairsMissingRemainingSecondsColumn(t *testing.T) {
	db := setupRedemptionTestDB(t)
	user := &identityschema.User{Id: 8821, Username: "blind_box_legacy_schema_user", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)
	var prop *commerceschema.BlindBoxProp
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		var err error
		prop, err = createBlindBoxPropTx(tx, user.Id, 9201, "0.9 倍率卡")
		return err
	}))
	require.NoError(t, db.Migrator().DropColumn(&commerceschema.BlindBoxProp{}, "RemainingSeconds"))

	props, err := ListUserBlindBoxProps(user.Id)
	require.NoError(t, err)
	require.Len(t, props, 1)
	require.Equal(t, prop.Id, props[0].Id)
	require.True(t, db.Migrator().HasColumn(&commerceschema.BlindBoxProp{}, "RemainingSeconds"))
}

func TestConvertBlindBoxDiscountPropBothDirections(t *testing.T) {
	db := setupRedemptionTestDB(t)
	user := &identityschema.User{Id: 8809, Username: "blind_box_prop_conversion_user", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)

	var prop *commerceschema.BlindBoxProp
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		var err error
		prop, err = createBlindBoxPropTx(tx, user.Id, 9001, "充值九折卡")
		return err
	}))

	converted, err := ConvertBlindBoxDiscountProp(user.Id, prop.Id, commerceschema.BlindBoxPropTypeSubscriptionDiscount90)
	require.NoError(t, err)
	require.Equal(t, commerceschema.BlindBoxPropTypeSubscriptionDiscount90, converted.PropType)
	require.Equal(t, "套餐九折卡", converted.Title)
	require.InDelta(t, 0.10, converted.DiscountRate, 0.0001)
	require.Equal(t, commerceschema.BlindBoxPropStatusAvailable, converted.Status)

	converted, err = ConvertBlindBoxDiscountProp(user.Id, prop.Id, commerceschema.BlindBoxPropTypeTopupDiscount90)
	require.NoError(t, err)
	require.Equal(t, commerceschema.BlindBoxPropTypeTopupDiscount90, converted.PropType)
	require.Equal(t, "充值九折卡", converted.Title)
	var logs []auditschema.Log
	require.NoError(t, db.Where("user_id = ? AND type = ?", user.Id, auditschema.LogTypeManage).Order("id asc").Find(&logs).Error)
	require.Len(t, logs, 2)
	require.Contains(t, logs[0].Content, "充值九折卡")
	require.Contains(t, logs[0].Content, "套餐九折卡")
}

func TestConvertBlindBoxDiscountPropRejectsReservedCard(t *testing.T) {
	db := setupRedemptionTestDB(t)
	user := &identityschema.User{Id: 8808, Username: "blind_box_prop_reserved_conversion_user", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)
	prop := &commerceschema.BlindBoxProp{
		UserId: user.Id, OpenRecordId: 9002, PropType: commerceschema.BlindBoxPropTypeTopupDiscount90,
		Title: "充值九折卡", Status: commerceschema.BlindBoxPropStatusReserved, DiscountRate: 0.10, Multiplier: 1,
	}
	require.NoError(t, db.Create(prop).Error)

	_, err := ConvertBlindBoxDiscountProp(user.Id, prop.Id, commerceschema.BlindBoxPropTypeSubscriptionDiscount90)
	require.ErrorContains(t, err, "only available")
}

func TestConvertBlindBoxDiscountPropRejectsAnotherUsersCard(t *testing.T) {
	db := setupRedemptionTestDB(t)
	owner := &identityschema.User{
		Id: 8806, Username: "blind_box_prop_conversion_owner", Status: constant.UserStatusEnabled,
		AffCode: "blind-box-conversion-owner",
	}
	other := &identityschema.User{
		Id: 8807, Username: "blind_box_prop_conversion_other", Status: constant.UserStatusEnabled,
		AffCode: "blind-box-conversion-other",
	}
	require.NoError(t, db.Create(owner).Error)
	require.NoError(t, db.Create(other).Error)
	prop := &commerceschema.BlindBoxProp{
		UserId: owner.Id, OpenRecordId: 9003, PropType: commerceschema.BlindBoxPropTypeTopupDiscount90,
		Title: "充值九折卡", Status: commerceschema.BlindBoxPropStatusAvailable, DiscountRate: 0.10, Multiplier: 1,
	}
	require.NoError(t, db.Create(prop).Error)

	_, err := ConvertBlindBoxDiscountProp(other.Id, prop.Id, commerceschema.BlindBoxPropTypeSubscriptionDiscount90)
	require.Error(t, err)
	var saved commerceschema.BlindBoxProp
	require.NoError(t, db.First(&saved, prop.Id).Error)
	require.Equal(t, commerceschema.BlindBoxPropTypeTopupDiscount90, saved.PropType)
}

func TestZeroHourPropActivatesUserScopedGroup(t *testing.T) {
	db := setupRedemptionTestDB(t)
	user := &identityschema.User{Id: 8811, Username: "zero_hour_user", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)

	var prop *commerceschema.BlindBoxProp
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		var err error
		prop, err = createBlindBoxPropTx(tx, user.Id, 1, "1 小时 0 倍率卡")
		return err
	}))
	require.NotNil(t, prop)

	activated, err := ActivateBlindBoxProp(user.Id, prop.Id)
	require.NoError(t, err)
	assert.Equal(t, commerceschema.BlindBoxPropTypeZeroHourMultiplier, activated.PropType)
	assert.Equal(t, int64(60*60), activated.DurationSeconds)
	assert.True(t, IsZeroHourGroupActive(user.Id))

	overview, err := BuildZeroHourOverview(user.Id)
	require.NoError(t, err)
	assert.True(t, overview.Active)
	assert.Equal(t, zeroHourBaseProbability, overview.CurrentProbability)
}

func TestZeroHourPropPausesAndResumesWithoutLosingRemainingTime(t *testing.T) {
	db := setupRedemptionTestDB(t)
	user := &identityschema.User{Id: 8816, Username: "pausable_zero_hour_user", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)

	var prop *commerceschema.BlindBoxProp
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		var err error
		prop, err = createBlindBoxPropTx(tx, user.Id, 2, "1 小时 0 倍率卡")
		return err
	}))

	activated, err := ActivateBlindBoxProp(user.Id, prop.Id)
	require.NoError(t, err)
	assert.Equal(t, commerceschema.BlindBoxPropStatusActive, activated.Status)
	assert.True(t, IsZeroHourGroupActive(user.Id))

	paused, err := PauseBlindBoxProp(user.Id, prop.Id)
	require.NoError(t, err)
	assert.Equal(t, commerceschema.BlindBoxPropStatusPaused, paused.Status)
	assert.Positive(t, paused.RemainingSeconds)
	assert.False(t, IsZeroHourGroupActive(user.Id))
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		assert.True(t, hasAvailableOrActiveZeroHourPropTx(tx, user.Id))
		return nil
	}))

	resumed, err := ActivateBlindBoxProp(user.Id, prop.Id)
	require.NoError(t, err)
	assert.Equal(t, commerceschema.BlindBoxPropStatusActive, resumed.Status)
	assert.Greater(t, resumed.ExpiresAt, platformruntime.GetTimestamp())
	assert.True(t, IsZeroHourGroupActive(user.Id))
}

func TestZeroHourProbabilityCapsAtConfiguredMaximum(t *testing.T) {
	assert.Equal(t, zeroHourBaseProbability, zeroHourProbability(0))
	assert.InDelta(t, 0.000541, zeroHourProbability(90), 0.0000000001)
	assert.Equal(t, zeroHourProbabilityCap, zeroHourProbability(zeroHourProgressCap))
	assert.Equal(t, zeroHourProbabilityCap, zeroHourProbability(zeroHourProgressCap+100))
}

func TestZeroHourUsageProgressAccumulatesWholeDollars(t *testing.T) {
	db := setupRedemptionTestDB(t)
	user := &identityschema.User{Id: 8813, Username: "zero_hour_progress_user", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)

	quotaPerUnit := int(platformruntime.QuotaPerUnit)
	RecordBlindBoxZeroHourUsage(user.Id, quotaPerUnit/2)
	RecordBlindBoxZeroHourUsage(user.Id, quotaPerUnit)

	overview, err := BuildZeroHourOverview(user.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(1), overview.Points)
}

func TestZeroHourPaidBlindBoxAddsFiveProgressPoints(t *testing.T) {
	db := setupRedemptionTestDB(t)
	user := &identityschema.User{Id: 8814, Username: "zero_hour_paid_box_user", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)

	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		state, err := getOrCreateZeroHourStateTx(tx, user.Id)
		if err != nil {
			return err
		}
		return addZeroHourProgressTx(tx, state, zeroHourProgressPerPaidOpen)
	}))

	overview, err := BuildZeroHourOverview(user.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(5), overview.Points)
}

func TestExpiredZeroHourPropDoesNotBlockAnotherCard(t *testing.T) {
	db := setupRedemptionTestDB(t)
	user := &identityschema.User{Id: 8812, Username: "expired_zero_hour_user", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)

	expired := &commerceschema.BlindBoxProp{
		UserId:          user.Id,
		PropType:        commerceschema.BlindBoxPropTypeZeroHourMultiplier,
		Title:           "1 小时 0 倍率卡",
		Status:          commerceschema.BlindBoxPropStatusActive,
		DurationSeconds: zeroHourDurationSeconds,
		ActivatedAt:     time.Now().Add(-2 * time.Hour).Unix(),
		ExpiresAt:       time.Now().Add(-time.Hour).Unix(),
	}
	require.NoError(t, db.Create(expired).Error)

	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		assert.False(t, hasAvailableOrActiveZeroHourPropTx(tx, user.Id))
		assert.False(t, hasActiveZeroHourPropTx(tx, user.Id))
		return nil
	}))
}

func TestMonthlyPassPropPausesAndResumesWithoutLosingRemainingTime(t *testing.T) {
	db := setupRedemptionTestDB(t)
	user := &identityschema.User{Id: 8815, Username: "monthly_pass_user", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)
	plan := &commerceschema.SubscriptionPlan{
		Id: 8815, Title: "Standard monthly", PlanType: commerceschema.SubscriptionPlanTypeMonthly,
		MembershipTier: commerceschema.SubscriptionMembershipTierStandard,
	}
	require.NoError(t, db.Create(plan).Error)

	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return awardMonthlyPassPropTx(tx, user.Id, plan, "monthly-pass-test:8815")
	}))
	var prop commerceschema.BlindBoxProp
	require.NoError(t, db.Where("user_id = ?", user.Id).First(&prop).Error)
	assert.Equal(t, int64(30*60), prop.RemainingSeconds)

	activated, err := ActivateBlindBoxProp(user.Id, prop.Id)
	require.NoError(t, err)
	assert.Equal(t, commerceschema.BlindBoxPropStatusActive, activated.Status)
	assert.True(t, IsMonthlyPassMultiplierActive(user.Id))

	paused, err := PauseBlindBoxProp(user.Id, prop.Id)
	require.NoError(t, err)
	assert.Equal(t, commerceschema.BlindBoxPropStatusPaused, paused.Status)
	assert.Positive(t, paused.RemainingSeconds)
	assert.False(t, IsMonthlyPassMultiplierActive(user.Id))

	resumed, err := ActivateBlindBoxProp(user.Id, prop.Id)
	require.NoError(t, err)
	assert.Equal(t, commerceschema.BlindBoxPropStatusActive, resumed.Status)
	assert.Greater(t, resumed.ExpiresAt, platformruntime.GetTimestamp())
}

func TestMultiplierCardsUseProChannelGroup(t *testing.T) {
	platformconfig.OptionMapRWMutex.Lock()
	original := platformconfig.OptionMap
	platformconfig.OptionMap = map[string]string{}
	platformconfig.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		platformconfig.OptionMapRWMutex.Lock()
		platformconfig.OptionMap = original
		platformconfig.OptionMapRWMutex.Unlock()
	})

	assert.Equal(t, DefaultMultiplierCardRouteGroup, MultiplierCardRouteGroup())

	platformconfig.OptionMapRWMutex.Lock()
	platformconfig.OptionMap[MultiplierCardRouteGroupOptionKey] = "倍率卡专属组"
	platformconfig.OptionMapRWMutex.Unlock()
	assert.Equal(t, "倍率卡专属组", MultiplierCardRouteGroup())
}

func TestBlindBoxPointOneMultiplierIsUniversalAndPausable(t *testing.T) {
	db := setupRedemptionTestDB(t)
	user := &identityschema.User{Id: 8819, Username: "blind_box_point_one", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)

	var prop *commerceschema.BlindBoxProp
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		var err error
		prop, err = createBlindBoxPropTx(tx, user.Id, 9901, "0.1 倍率卡")
		return err
	}))
	require.Equal(t, commerceschema.BlindBoxPropTypeConsumeDiscount10, prop.PropType)
	require.Equal(t, int64(15*60), prop.DurationSeconds)

	active, err := ActivateBlindBoxProp(user.Id, prop.Id)
	require.NoError(t, err)
	require.Equal(t, commerceschema.BlindBoxPropStatusActive, active.Status)
	paused, err := PauseBlindBoxProp(user.Id, prop.Id)
	require.NoError(t, err)
	require.Equal(t, commerceschema.BlindBoxPropStatusPaused, paused.Status)
}

func TestMigrateLegacyBlindBoxMultiplierPropsKeepsMonthlyPassCardsSeparate(t *testing.T) {
	db := setupRedemptionTestDB(t)
	legacyBlindBox := &commerceschema.BlindBoxProp{
		UserId: 8820, OpenRecordId: 9902, PropType: commerceschema.BlindBoxPropTypeMonthlyPassMultiplier,
		Title: "0.10 倍率体验卡", Status: commerceschema.BlindBoxPropStatusAvailable,
		Multiplier: 0.1, DurationSeconds: 15 * 60,
	}
	monthlyPass := &commerceschema.BlindBoxProp{
		UserId: 8820, PropType: commerceschema.BlindBoxPropTypeMonthlyPassMultiplier,
		Title: "30 分钟 0.1 倍率卡", Status: commerceschema.BlindBoxPropStatusAvailable,
		Multiplier: 0.1, DurationSeconds: 30 * 60, BenefitReference: "monthly-pass:test",
	}
	require.NoError(t, db.Create(legacyBlindBox).Error)
	require.NoError(t, db.Create(monthlyPass).Error)
	require.NoError(t, migrateLegacyBlindBoxMultiplierProps())

	require.NoError(t, db.First(legacyBlindBox, legacyBlindBox.Id).Error)
	require.Equal(t, commerceschema.BlindBoxPropTypeConsumeDiscount10, legacyBlindBox.PropType)
	require.Equal(t, "0.1 倍率卡", legacyBlindBox.Title)
	require.NoError(t, db.First(monthlyPass, monthlyPass.Id).Error)
	require.Equal(t, commerceschema.BlindBoxPropTypeMonthlyPassMultiplier, monthlyPass.PropType)
}

func TestBackfillActiveMonthlyPassBenefitsIsIdempotent(t *testing.T) {
	db := setupRedemptionTestDB(t)
	now := platformruntime.GetTimestamp()
	user := &identityschema.User{Id: 8816, Username: "monthly_pass_backfill", Status: constant.UserStatusEnabled}
	plan := &commerceschema.SubscriptionPlan{
		Id: 8816, Title: "Pro monthly", PlanType: commerceschema.SubscriptionPlanTypeMonthly,
		MembershipTier: commerceschema.SubscriptionMembershipTierPro,
	}
	subscription := &commerceschema.UserSubscription{
		Id: 8816, UserId: user.Id, PlanId: plan.Id, Status: "active", StartTime: now - 60, EndTime: now + 86400,
	}
	require.NoError(t, db.Create(user).Error)
	require.NoError(t, db.Create(plan).Error)
	require.NoError(t, db.Create(subscription).Error)

	require.NoError(t, BackfillActiveMonthlyPassBenefits())
	require.NoError(t, BackfillActiveMonthlyPassBenefits())
	var props []commerceschema.BlindBoxProp
	require.NoError(t, db.Where("user_id = ? AND prop_type = ?", user.Id, commerceschema.BlindBoxPropTypeMonthlyPassMultiplier).Find(&props).Error)
	require.Len(t, props, 1)
	assert.Equal(t, int64(45*60), props[0].DurationSeconds)
	assert.Equal(t, "monthly-pass-backfill-20260811:8816", props[0].BenefitReference)
}

func TestBackfillActiveMonthlyPassBenefitsSkipsIneligibleSubscriptions(t *testing.T) {
	db := setupRedemptionTestDB(t)
	now := platformruntime.GetTimestamp()
	user := &identityschema.User{Id: 8817, Username: "monthly_pass_skip", Status: constant.UserStatusEnabled}
	dayPlan := &commerceschema.SubscriptionPlan{Id: 8817, Title: "day", PlanType: commerceschema.SubscriptionPlanTypeDaily}
	monthPlan := &commerceschema.SubscriptionPlan{
		Id: 8818, Title: "Lite monthly", PlanType: commerceschema.SubscriptionPlanTypeMonthly,
		MembershipTier: commerceschema.SubscriptionMembershipTierLite,
	}
	require.NoError(t, db.Create(user).Error)
	require.NoError(t, db.Create(dayPlan).Error)
	require.NoError(t, db.Create(monthPlan).Error)
	require.NoError(t, db.Create(&commerceschema.UserSubscription{Id: 8817, UserId: user.Id, PlanId: dayPlan.Id, Status: "active", EndTime: now + 86400}).Error)
	require.NoError(t, db.Create(&commerceschema.UserSubscription{Id: 8818, UserId: user.Id, PlanId: monthPlan.Id, Status: "active", EndTime: now + 86400}).Error)

	require.NoError(t, BackfillActiveMonthlyPassBenefits())
	var props []commerceschema.BlindBoxProp
	require.NoError(t, db.Where("user_id = ? AND prop_type = ?", user.Id, commerceschema.BlindBoxPropTypeMonthlyPassMultiplier).Find(&props).Error)
	require.Len(t, props, 1)
	assert.Equal(t, int64(15*60), props[0].DurationSeconds)
}
