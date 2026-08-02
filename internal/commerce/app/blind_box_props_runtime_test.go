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
