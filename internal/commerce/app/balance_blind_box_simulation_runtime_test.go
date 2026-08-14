package app

import (
	"testing"

	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/require"
)

func TestBalanceBlindBoxSimulationDoesNotMutateRealEconomy(t *testing.T) {
	db := setupRedemptionTestDB(t)
	setBalanceBlindBoxTestSetting(t, 10)
	user := createBalanceBlindBoxTestUser(t, db, 8921, "SIM001", 100)
	admin := createBalanceBlindBoxTestUser(t, db, 8922, "SIM002", 0)

	session, err := StartBalanceBlindBoxSimulation(user.Id, admin.Id, AdminBalanceBlindBoxSimulationRequest{
		DurationMinutes: 30,
		Reason:          "probability verification",
	})
	require.NoError(t, err)
	require.True(t, session.Active)

	result, err := SimulateBalanceBlindBoxes(user.Id, "simulation-batch-1", 100)
	require.NoError(t, err)
	require.Len(t, result.Records, 100)
	require.Equal(t, 100, result.Simulation.DrawCount)
	require.InDelta(t, 1500, result.Simulation.SimulatedCostUSD, 0.000001)
	for _, record := range result.Records {
		require.True(t, record.Simulation)
		require.Negative(t, record.Id)
	}

	var savedUserQuota int
	require.NoError(t, db.Model(user).Select("quota").Scan(&savedUserQuota).Error)
	require.Equal(t, user.Quota, savedUserQuota)
	for _, model := range []any{
		&commerceschema.BalanceBlindBoxPurchase{},
		&commerceschema.BalanceBlindBoxItem{},
		&commerceschema.BlindBoxOpenRecord{},
		&commerceschema.BlindBoxProp{},
		&commerceschema.BalanceBlindBoxPityState{},
	} {
		var count int64
		require.NoError(t, db.Model(model).Count(&count).Error)
		require.Zero(t, count)
	}

	replayed, err := SimulateBalanceBlindBoxes(user.Id, "simulation-batch-1", 100)
	require.NoError(t, err)
	require.Equal(t, result.Records, replayed.Records)
	require.Equal(t, 100, replayed.Simulation.DrawCount)

	_, err = SimulateBalanceBlindBoxes(admin.Id, "simulation-batch-1", 100)
	require.ErrorContains(t, err, "请求冲突")
}

func TestBalanceBlindBoxSimulationStopsAndExpires(t *testing.T) {
	db := setupRedemptionTestDB(t)
	setBalanceBlindBoxTestSetting(t, 10)
	user := createBalanceBlindBoxTestUser(t, db, 8923, "SIM003", 100)
	admin := createBalanceBlindBoxTestUser(t, db, 8924, "SIM004", 0)

	_, err := StartBalanceBlindBoxSimulation(user.Id, admin.Id, AdminBalanceBlindBoxSimulationRequest{DurationMinutes: 10})
	require.NoError(t, err)
	stopped, err := StopBalanceBlindBoxSimulation(user.Id, admin.Id)
	require.NoError(t, err)
	require.False(t, stopped.Active)
	_, err = SimulateBalanceBlindBoxes(user.Id, "simulation-after-stop", 1)
	require.ErrorContains(t, err, "不存在或已到期")

	_, err = StartBalanceBlindBoxSimulation(user.Id, admin.Id, AdminBalanceBlindBoxSimulationRequest{DurationMinutes: 10})
	require.NoError(t, err)
	require.NoError(t, db.Model(&commerceschema.BalanceBlindBoxSimulationSession{}).
		Where("user_id = ? AND status = ?", user.Id, commerceschema.BalanceBlindBoxSimulationStatusActive).
		Update("expires_at", platformruntime.GetTimestamp()-1).Error)
	_, err = SimulateBalanceBlindBoxes(user.Id, "simulation-after-expiry", 1)
	require.ErrorContains(t, err, "不存在或已到期")

	overview, err := GetBalanceBlindBoxSimulationOverview(user.Id)
	require.NoError(t, err)
	require.False(t, overview.Active)
}

func TestBalanceBlindBoxSimulationRejectsUnsafeDuration(t *testing.T) {
	db := setupRedemptionTestDB(t)
	user := createBalanceBlindBoxTestUser(t, db, 8925, "SIM005", 0)
	admin := createBalanceBlindBoxTestUser(t, db, 8926, "SIM006", 0)

	_, err := StartBalanceBlindBoxSimulation(user.Id, admin.Id, AdminBalanceBlindBoxSimulationRequest{DurationMinutes: 0})
	require.Error(t, err)
	_, err = StartBalanceBlindBoxSimulation(user.Id, admin.Id, AdminBalanceBlindBoxSimulationRequest{DurationMinutes: maxBalanceBlindBoxSimulationMinutes + 1})
	require.Error(t, err)
}

func TestBalanceBlindBoxSimulationCopiesRealPityWithoutMutatingIt(t *testing.T) {
	db := setupRedemptionTestDB(t)
	setBalanceBlindBoxTestSetting(t, 10)
	user := createBalanceBlindBoxTestUser(t, db, 8927, "SIM007", 0)
	admin := createBalanceBlindBoxTestUser(t, db, 8928, "SIM008", 0)
	realPity := commerceschema.BalanceBlindBoxPityState{
		UserId: user.Id, ConsecutiveUnder6USD: 3, ConsecutiveUnder35USD: 4,
	}
	require.NoError(t, db.Create(&realPity).Error)
	require.NoError(t, db.Create(&commerceschema.BalanceBlindBoxItem{
		PurchaseId: 1, PurchaseUserId: user.Id, OwnerUserId: user.Id,
		PoolVersion: "test", RewardType: commerceschema.BlindBoxRewardTypeQuota,
		RewardTier: "test", RewardUSD: 1, CreditAmount: 1,
		RewardTitle: "test", RewardWalletType: "default", GuaranteeType: "none",
	}).Error)

	_, err := StartBalanceBlindBoxSimulation(user.Id, admin.Id, AdminBalanceBlindBoxSimulationRequest{DurationMinutes: 10})
	require.NoError(t, err)
	var session commerceschema.BalanceBlindBoxSimulationSession
	require.NoError(t, db.Where("user_id = ?", user.Id).First(&session).Error)
	require.Equal(t, 3, session.ConsecutiveUnder6USD)
	require.Equal(t, 4, session.ConsecutiveUnder35USD)
	require.False(t, session.FirstDrawEligible)

	_, err = SimulateBalanceBlindBoxes(user.Id, "simulation-pity-copy", 10)
	require.NoError(t, err)
	var unchanged commerceschema.BalanceBlindBoxPityState
	require.NoError(t, db.Where("user_id = ?", user.Id).First(&unchanged).Error)
	require.Equal(t, realPity.ConsecutiveUnder6USD, unchanged.ConsecutiveUnder6USD)
	require.Equal(t, realPity.ConsecutiveUnder35USD, unchanged.ConsecutiveUnder35USD)
}
