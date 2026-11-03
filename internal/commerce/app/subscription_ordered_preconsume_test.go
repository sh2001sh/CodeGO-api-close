package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/constant"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
)

type orderedSubscriptionFixture struct {
	userID int
	legacy *commerceschema.UserSubscription
	pack   *commerceschema.UserSubscription
}

func createOrderedSubscriptionFixture(t *testing.T, userID int) orderedSubscriptionFixture {
	t.Helper()
	database := db.DB
	user := &identityschema.User{
		Id: userID, Username: fmt.Sprintf("ordered-subscription-%d", userID),
		AffCode: fmt.Sprintf("ORDER-%d", userID), Status: constant.UserStatusEnabled,
	}
	require.NoError(t, database.Create(user).Error)
	plan := insertSubscriptionResetAppTestPlan(t, userID+1, 0, 10_000)
	legacy := &commerceschema.UserSubscription{
		Id: userID + 2, UserId: userID, PlanId: plan.Id,
		AmountTotal: 10_000, Status: "active", EndTime: time.Now().Add(time.Hour).Unix(),
	}
	require.NoError(t, database.Create(legacy).Error)
	pack, err := createCreditPackBalanceTx(database, userID, plan.Id, 10_000, 9_000, 6000, fmt.Sprintf("ordered-pack-%d", userID))
	require.NoError(t, err)
	return orderedSubscriptionFixture{userID: userID, legacy: legacy, pack: pack}
}

func setSubscriptionOrder(t *testing.T, userID int, ids []int) {
	t.Helper()
	_, err := UpdateSubscriptionPreference(userID, UpdateSubscriptionPreferenceRequest{
		BillingPreference: "subscription_first", SubscriptionOrderIds: ids,
	})
	require.NoError(t, err)
}

func TestPreConsumeOrderedSubscriptionHonorsCrossTypeOrder(t *testing.T) {
	setupRedemptionTestDB(t)
	ensureSubscriptionPreConsumeRecordSchema(t)

	for index, test := range []struct {
		name        string
		order       func(orderedSubscriptionFixture) []int
		wantPack    bool
		allowPack   bool
		allowLegacy bool
	}{
		{name: "monthly_first", order: func(f orderedSubscriptionFixture) []int { return []int{f.legacy.Id, f.pack.Id} }, allowPack: true, allowLegacy: true},
		{name: "pack_first", order: func(f orderedSubscriptionFixture) []int { return []int{f.pack.Id, f.legacy.Id} }, wantPack: true, allowPack: true, allowLegacy: true},
		{name: "unsupported_pack_is_skipped", order: func(f orderedSubscriptionFixture) []int { return []int{f.pack.Id, f.legacy.Id} }, allowLegacy: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := createOrderedSubscriptionFixture(t, 81000+index*10)
			setSubscriptionOrder(t, fixture.userID, test.order(fixture))
			result, requestID, err := PreConsumeOrderedSubscription(
				"ordered-legacy-"+test.name, "ordered-pack-"+test.name,
				fixture.userID, "gpt-5", 100, 200, test.allowLegacy, test.allowPack,
			)
			require.NoError(t, err)
			require.NotNil(t, result)
			if test.wantPack {
				require.Equal(t, fixture.pack.Id, result.UserSubscriptionId)
				require.Equal(t, "ordered-pack-"+test.name, requestID)
				require.EqualValues(t, 200, result.PreConsumed)
			} else {
				require.Equal(t, fixture.legacy.Id, result.UserSubscriptionId)
				require.Equal(t, "ordered-legacy-"+test.name, requestID)
				require.EqualValues(t, 100, result.PreConsumed)
			}
		})
	}
}

func TestPreConsumeOrderedSubscriptionFallsBackInExactOrderAndReplays(t *testing.T) {
	database := setupRedemptionTestDB(t)
	ensureSubscriptionPreConsumeRecordSchema(t)
	fixture := createOrderedSubscriptionFixture(t, 81100)
	secondPlan := insertSubscriptionResetAppTestPlan(t, 81110, 0, 10_000)
	secondLegacy := &commerceschema.UserSubscription{
		Id: 81111, UserId: fixture.userID, PlanId: secondPlan.Id,
		AmountTotal: 10_000, Status: "active", EndTime: time.Now().Add(2 * time.Hour).Unix(),
	}
	require.NoError(t, database.Create(secondLegacy).Error)
	require.NoError(t, database.Model(fixture.legacy).Update("amount_used", fixture.legacy.AmountTotal).Error)
	setSubscriptionOrder(t, fixture.userID, []int{fixture.legacy.Id, fixture.pack.Id, secondLegacy.Id})

	result, requestID, err := PreConsumeOrderedSubscription(
		"ordered-fallback-legacy", "ordered-fallback-pack", fixture.userID, "gpt-5", 100, 200, true, true,
	)
	require.NoError(t, err)
	require.Equal(t, fixture.pack.Id, result.UserSubscriptionId)
	require.Equal(t, "ordered-fallback-pack", requestID)

	replay, replayRequestID, err := PreConsumeOrderedSubscription(
		"ordered-fallback-legacy", "ordered-fallback-pack", fixture.userID, "gpt-5", 100, 200, true, true,
	)
	require.NoError(t, err)
	require.Equal(t, result.UserSubscriptionId, replay.UserSubscriptionId)
	require.Equal(t, result.ReservationID, replay.ReservationID)
	require.Equal(t, requestID, replayRequestID)

	var savedSecond commerceschema.UserSubscription
	require.NoError(t, database.First(&savedSecond, secondLegacy.Id).Error)
	require.Zero(t, savedSecond.AmountUsed, "the candidate after the selected pack must not be consumed")
}

func TestPreConsumeOrderedSubscriptionKeepsPackFirstDefault(t *testing.T) {
	setupRedemptionTestDB(t)
	ensureSubscriptionPreConsumeRecordSchema(t)
	fixture := createOrderedSubscriptionFixture(t, 81200)

	result, requestID, err := PreConsumeOrderedSubscription(
		"ordered-default-legacy", "ordered-default-pack", fixture.userID, "gpt-5", 100, 200, true, true,
	)
	require.NoError(t, err)
	require.Equal(t, fixture.pack.Id, result.UserSubscriptionId)
	require.Equal(t, "ordered-default-pack", requestID)

	_, _, err = PreConsumeOrderedSubscription(
		"ordered-default-legacy", "ordered-default-pack", fixture.userID, "gpt-5", 100, 200, true, false,
	)
	require.ErrorContains(t, err, "no longer allowed", "a replay must never switch away from its original source")
}
