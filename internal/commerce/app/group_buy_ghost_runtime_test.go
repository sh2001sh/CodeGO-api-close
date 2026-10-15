package app

import (
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"github.com/sh2001sh/new-api/constant"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"strings"
	"testing"
	"time"
)

func TestInitGhostUsers(t *testing.T) {
	db := setupRedemptionTestDB(t)

	ids, err := initGhostUsersDB()
	require.NoError(t, err)
	assert.Len(t, ids, 3)

	for _, userID := range ids {
		var user identityschema.User
		require.NoError(t, db.Where("id = ?", userID).First(&user).Error)
		assert.True(t, strings.HasPrefix(user.Username, "ghost_user_"))
		assert.Equal(t, "ghost", user.Group)
		assert.Equal(t, constant.UserStatusEnabled, user.Status)
	}

	secondIDs, err := initGhostUsersDB()
	require.NoError(t, err)
	assert.Len(t, secondIDs, 3)
}

func TestAddGhostMemberToNewOrder(t *testing.T) {
	db := setupRedemptionTestDB(t)

	ghostUserIDs, err := initGhostUsersDB()
	require.NoError(t, err)
	require.NotEmpty(t, ghostUserIDs)

	plan := &commerceschema.SubscriptionPlan{
		Id:              9901,
		Title:           "Test月卡",
		PriceAmount:     99,
		Currency:        "CNY",
		DurationUnit:    commerceschema.SubscriptionDurationMonth,
		DurationValue:   1,
		Enabled:         true,
		GroupBuyEnabled: true,
		GroupBuyBonus2:  20,
		GroupBuyBonus3:  35,
		GroupBuyBonus5:  50,
		TotalAmount:     int64(platformruntime.QuotaPerUnit * 100),
	}
	require.NoError(t, db.Create(plan).Error)

	realUser := &identityschema.User{
		Id:       9902,
		Username: "real_user_test",
		Status:   constant.UserStatusEnabled,
		Quota:    1000,
		AffCode:  "RU9902",
	}
	require.NoError(t, db.Create(realUser).Error)

	now := time.Now()
	order := &commerceschema.GroupBuyOrder{
		Id:           9903,
		InitiatorId:  realUser.Id,
		PlanId:       plan.Id,
		Status:       commerceschema.GroupBuyStatusPending,
		TargetCount:  5,
		CurrentCount: 1,
		ExpiresAt:    now.Add(48 * time.Hour).Unix(),
	}
	require.NoError(t, db.Create(order).Error)

	realMember := &commerceschema.GroupBuyMember{
		GroupBuyId:         order.Id,
		UserId:             realUser.Id,
		OrderId:            1,
		UserSubscriptionId: 0,
		BonusGranted:       false,
	}
	require.NoError(t, db.Create(realMember).Error)

	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return AddGhostMemberToNewOrder(tx, order.Id)
	}))

	var updatedOrder commerceschema.GroupBuyOrder
	require.NoError(t, db.Where("id = ?", order.Id).First(&updatedOrder).Error)
	assert.Equal(t, 2, updatedOrder.CurrentCount)

	var members []commerceschema.GroupBuyMember
	require.NoError(t, db.Where("group_buy_id = ?", order.Id).Find(&members).Error)
	require.Len(t, members, 2)

	ghostCount := 0
	for _, member := range members {
		if member.OrderId == 0 && member.BonusGranted {
			ghostCount++
			assert.Contains(t, ghostUserIDs, member.UserId)
		}
	}
	assert.Equal(t, 1, ghostCount)
}

func TestEnsureGhostGroupBuys_NoActiveOrders_CreatesTwoGhostOrders(t *testing.T) {
	db := setupRedemptionTestDB(t)

	ghostUserIDs, err := initGhostUsersDB()
	require.NoError(t, err)
	require.NotEmpty(t, ghostUserIDs)

	plans := []*commerceschema.SubscriptionPlan{
		{Id: 9931, Title: "Lite月卡", PriceAmount: 29, Currency: "CNY", DurationUnit: commerceschema.SubscriptionDurationMonth, DurationValue: 1, Enabled: true, GroupBuyEnabled: true, GroupBuyBonus2: 20, TotalAmount: int64(platformruntime.QuotaPerUnit * 50)},
		{Id: 9932, Title: "Standard月卡", PriceAmount: 59, Currency: "CNY", DurationUnit: commerceschema.SubscriptionDurationMonth, DurationValue: 1, Enabled: true, GroupBuyEnabled: true, GroupBuyBonus2: 20, TotalAmount: int64(platformruntime.QuotaPerUnit * 100)},
		{Id: 9933, Title: "Pro月卡", PriceAmount: 99, Currency: "CNY", DurationUnit: commerceschema.SubscriptionDurationMonth, DurationValue: 1, Enabled: true, GroupBuyEnabled: true, GroupBuyBonus2: 20, TotalAmount: int64(platformruntime.QuotaPerUnit * 200)},
	}
	for _, plan := range plans {
		require.NoError(t, db.Create(plan).Error)
	}

	require.NoError(t, ensureGhostGroupBuysInternal())

	var count int64
	require.NoError(t, db.Model(&commerceschema.GroupBuyOrder{}).Where("status = ?", commerceschema.GroupBuyStatusPending).Count(&count).Error)
	assert.EqualValues(t, 2, count)

	var orders []commerceschema.GroupBuyOrder
	require.NoError(t, db.Where("status = ?", commerceschema.GroupBuyStatusPending).Find(&orders).Error)
	for _, order := range orders {
		assert.Contains(t, ghostUserIDs, order.InitiatorId)
		var members []commerceschema.GroupBuyMember
		require.NoError(t, db.Where("group_buy_id = ?", order.Id).Find(&members).Error)
		assert.Len(t, members, 1)
		assert.Equal(t, 0, members[0].OrderId)
		assert.True(t, members[0].BonusGranted)
	}
}

func TestEnsureGhostGroupBuys_OneActiveOrder_CreatesOneGhostOrder(t *testing.T) {
	db := setupRedemptionTestDB(t)

	_, err := initGhostUsersDB()
	require.NoError(t, err)

	plans := []*commerceschema.SubscriptionPlan{
		{Id: 9941, Title: "Lite月卡", PriceAmount: 29, Currency: "CNY", DurationUnit: commerceschema.SubscriptionDurationMonth, DurationValue: 1, Enabled: true, GroupBuyEnabled: true, GroupBuyBonus2: 20, TotalAmount: int64(platformruntime.QuotaPerUnit * 50)},
		{Id: 9942, Title: "Pro月卡", PriceAmount: 99, Currency: "CNY", DurationUnit: commerceschema.SubscriptionDurationMonth, DurationValue: 1, Enabled: true, GroupBuyEnabled: true, GroupBuyBonus2: 20, TotalAmount: int64(platformruntime.QuotaPerUnit * 200)},
	}
	for _, plan := range plans {
		require.NoError(t, db.Create(plan).Error)
	}

	existing := &commerceschema.GroupBuyOrder{
		Id: 9943, InitiatorId: 1, PlanId: plans[0].Id,
		Status: commerceschema.GroupBuyStatusPending, TargetCount: 5, CurrentCount: 1,
		ExpiresAt: time.Now().Add(48 * time.Hour).Unix(),
	}
	require.NoError(t, db.Create(existing).Error)

	require.NoError(t, ensureGhostGroupBuysInternal())

	var count int64
	require.NoError(t, db.Model(&commerceschema.GroupBuyOrder{}).Where("status = ?", commerceschema.GroupBuyStatusPending).Count(&count).Error)
	assert.EqualValues(t, 2, count)
}

func TestEnsureGhostGroupBuys_TwoOrMoreActiveOrders_CreatesNone(t *testing.T) {
	db := setupRedemptionTestDB(t)

	_, err := initGhostUsersDB()
	require.NoError(t, err)

	plans := []*commerceschema.SubscriptionPlan{
		{Id: 9951, Title: "Lite月卡", PriceAmount: 29, Currency: "CNY", DurationUnit: commerceschema.SubscriptionDurationMonth, DurationValue: 1, Enabled: true, GroupBuyEnabled: true, GroupBuyBonus2: 20, TotalAmount: int64(platformruntime.QuotaPerUnit * 50)},
		{Id: 9952, Title: "Pro月卡", PriceAmount: 99, Currency: "CNY", DurationUnit: commerceschema.SubscriptionDurationMonth, DurationValue: 1, Enabled: true, GroupBuyEnabled: true, GroupBuyBonus2: 20, TotalAmount: int64(platformruntime.QuotaPerUnit * 200)},
	}
	for _, plan := range plans {
		require.NoError(t, db.Create(plan).Error)
	}

	for index, plan := range plans {
		order := &commerceschema.GroupBuyOrder{
			Id: int64(9953 + index), InitiatorId: 1, PlanId: plan.Id,
			Status: commerceschema.GroupBuyStatusPending, TargetCount: 5, CurrentCount: 1,
			ExpiresAt: time.Now().Add(48 * time.Hour).Unix(),
		}
		require.NoError(t, db.Create(order).Error)
	}

	require.NoError(t, ensureGhostGroupBuysInternal())

	var count int64
	require.NoError(t, db.Model(&commerceschema.GroupBuyOrder{}).Where("status = ?", commerceschema.GroupBuyStatusPending).Count(&count).Error)
	assert.EqualValues(t, 2, count)
}
