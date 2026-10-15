package app

import (
	"testing"

	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReconcileSettledMonthlyGroupBuyBonusesScansOnlySettledMonthlyGroups(t *testing.T) {
	db := setupRedemptionTestDB(t)
	monthlyPlan := &commerceschema.SubscriptionPlan{
		Id:            9971,
		Title:         "月卡",
		DurationUnit:  commerceschema.SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
	}
	weeklyPlan := &commerceschema.SubscriptionPlan{
		Id:            9972,
		Title:         "周卡",
		DurationUnit:  commerceschema.SubscriptionDurationDay,
		DurationValue: 7,
		Enabled:       true,
	}
	require.NoError(t, db.Create(monthlyPlan).Error)
	require.NoError(t, db.Create(weeklyPlan).Error)

	orders := []*commerceschema.GroupBuyOrder{
		{Id: 99711, InitiatorId: 1, PlanId: monthlyPlan.Id, Status: commerceschema.GroupBuyStatusCompleted},
		{Id: 99712, InitiatorId: 1, PlanId: monthlyPlan.Id, Status: commerceschema.GroupBuyStatusExpired},
		{Id: 99713, InitiatorId: 1, PlanId: monthlyPlan.Id, Status: commerceschema.GroupBuyStatusPending},
		{Id: 99714, InitiatorId: 1, PlanId: weeklyPlan.Id, Status: commerceschema.GroupBuyStatusCompleted},
	}
	for _, order := range orders {
		require.NoError(t, db.Create(order).Error)
	}

	result, err := ReconcileSettledMonthlyGroupBuyBonuses()
	require.NoError(t, err)
	assert.Equal(t, 2, result.GroupsScanned)
	assert.Zero(t, result.MembersAdjusted)
}

func TestReconcileSettledMonthlyGroupBuyBonusesSkipsExpiredSubscriptions(t *testing.T) {
	db := setupRedemptionTestDB(t)
	plan := &commerceschema.SubscriptionPlan{
		Id:              9981,
		Title:           "月卡",
		DurationUnit:    commerceschema.SubscriptionDurationMonth,
		DurationValue:   1,
		Enabled:         true,
		GroupBuyEnabled: true,
		GroupBuyBonus2:  20,
	}
	require.NoError(t, db.Create(plan).Error)

	subscription := &commerceschema.UserSubscription{
		Id:     99811,
		UserId: 9981,
		PlanId: plan.Id,
		Status: "expired",
	}
	require.NoError(t, db.Create(subscription).Error)

	order := &commerceschema.GroupBuyOrder{
		Id: 99812, InitiatorId: subscription.UserId, PlanId: plan.Id, Status: commerceschema.GroupBuyStatusCompleted,
	}
	require.NoError(t, db.Create(order).Error)
	require.NoError(t, db.Create(&commerceschema.GroupBuyMember{
		GroupBuyId: order.Id, UserId: subscription.UserId, OrderId: 1, UserSubscriptionId: subscription.Id,
	}).Error)
	require.NoError(t, db.Create(&commerceschema.GroupBuyMember{
		GroupBuyId: order.Id, UserId: 9982, OrderId: 0, BonusGranted: true,
	}).Error)

	result, err := ReconcileSettledMonthlyGroupBuyBonuses()
	require.NoError(t, err)
	assert.Equal(t, 1, result.GroupsScanned)
	assert.Zero(t, result.MembersAdjusted)
}

func TestInspectSettledMonthlyGroupBuyBonusesReportsOnlyActiveMemberDifferences(t *testing.T) {
	db := setupRedemptionTestDB(t)
	plan := &commerceschema.SubscriptionPlan{
		Id:              9991,
		Title:           "月卡",
		DurationUnit:    commerceschema.SubscriptionDurationMonth,
		DurationValue:   1,
		Enabled:         true,
		GroupBuyEnabled: true,
		GroupBuyBonus2:  20,
	}
	require.NoError(t, db.Create(plan).Error)

	subscription := &commerceschema.UserSubscription{
		Id: 99911, UserId: 9991, PlanId: plan.Id, Status: "active",
	}
	require.NoError(t, db.Create(subscription).Error)
	order := &commerceschema.GroupBuyOrder{
		Id: 99912, InitiatorId: subscription.UserId, PlanId: plan.Id, Status: commerceschema.GroupBuyStatusCompleted,
	}
	require.NoError(t, db.Create(order).Error)
	require.NoError(t, db.Create(&commerceschema.GroupBuyMember{
		GroupBuyId: order.Id, UserId: subscription.UserId, OrderId: 1, UserSubscriptionId: subscription.Id,
	}).Error)
	require.NoError(t, db.Create(&commerceschema.GroupBuyMember{
		GroupBuyId: order.Id, UserId: 9992, OrderId: 0, BonusGranted: true,
	}).Error)

	result, err := InspectSettledMonthlyGroupBuyBonuses()
	require.NoError(t, err)
	assert.Equal(t, 1, result.GroupsScanned)
	assert.Equal(t, 1, result.MembersAdjusted)
	assert.Equal(t, 20.0, result.EligibleBonusUSD)
}
