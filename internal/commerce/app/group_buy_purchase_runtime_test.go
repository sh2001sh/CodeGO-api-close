package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/constant"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestValidateGroupBuyPurchaseRejectsDayPass(t *testing.T) {
	db := setupRedemptionTestDB(t)
	plan := &commerceschema.SubscriptionPlan{
		Id:              9799,
		Title:           "50刀日卡",
		PriceAmount:     8.9,
		Currency:        "CNY",
		DurationUnit:    commerceschema.SubscriptionDurationDay,
		DurationValue:   1,
		Enabled:         true,
		GroupBuyEnabled: true,
		TotalAmount:     int64(platformruntime.QuotaPerUnit * 50),
	}
	require.NoError(t, db.Create(plan).Error)

	err := ValidateGroupBuyPurchase(97991, plan.Id, commerceschema.SubscriptionPurchaseTypeGroupBuy, 0)
	assert.ErrorIs(t, err, ErrGroupBuyPlanNotEnabled)
}

func TestSettleGroupBuyOrderAddsBonusToSubscriptionInsteadOfWallet(t *testing.T) {
	db := setupRedemptionTestDB(t)

	plan := &commerceschema.SubscriptionPlan{
		Id:               9801,
		Title:            "Group Buy Bonus Plan",
		PriceAmount:      99,
		Currency:         "USD",
		DurationUnit:     commerceschema.SubscriptionDurationMonth,
		DurationValue:    1,
		Enabled:          true,
		GroupBuyEnabled:  true,
		GroupBuyBonus2:   20,
		GroupBuyBonus3:   35,
		GroupBuyBonus5:   50,
		TotalAmount:      int64(platformruntime.QuotaPerUnit * 100),
		PeriodAmount:     int64(platformruntime.QuotaPerUnit * 40),
		QuotaResetPeriod: commerceschema.SubscriptionResetMonthly,
	}
	require.NoError(t, db.Create(plan).Error)

	users := []*identityschema.User{
		{Id: 98011, Username: "group_buy_bonus_user_1", Status: constant.UserStatusEnabled, Quota: 777, AffCode: "GB98011"},
		{Id: 98012, Username: "group_buy_bonus_user_2", Status: constant.UserStatusEnabled, Quota: 888, AffCode: "GB98012"},
	}
	for _, user := range users {
		require.NoError(t, db.Create(user).Error)
	}

	now := time.Now()
	subs := []*commerceschema.UserSubscription{
		{
			Id:           98101,
			UserId:       users[0].Id,
			PlanId:       plan.Id,
			AmountTotal:  plan.TotalAmount,
			AmountUsed:   int64(platformruntime.QuotaPerUnit * 10),
			PeriodAmount: plan.PeriodAmount,
			PeriodUsed:   int64(platformruntime.QuotaPerUnit * 4),
			StartTime:    now.Add(-24 * time.Hour).Unix(),
			EndTime:      now.Add(24 * time.Hour).Unix(),
			Status:       "active",
		},
		{
			Id:           98102,
			UserId:       users[1].Id,
			PlanId:       plan.Id,
			AmountTotal:  plan.TotalAmount,
			AmountUsed:   int64(platformruntime.QuotaPerUnit * 8),
			PeriodAmount: plan.PeriodAmount,
			PeriodUsed:   int64(platformruntime.QuotaPerUnit * 3),
			StartTime:    now.Add(-24 * time.Hour).Unix(),
			EndTime:      now.Add(24 * time.Hour).Unix(),
			Status:       "active",
		},
	}
	for _, sub := range subs {
		require.NoError(t, db.Create(sub).Error)
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
			account, err := billingdomain.EnsureBillingAccountTx(tx, billingdomain.EnsureAccountParams{
				AccountType: "subscription", OwnerType: "user_subscription", OwnerID: int64(sub.Id), QuotaUnit: "quota",
			})
			if err != nil {
				return err
			}
			_, err = billingdomain.CreditAccountTx(tx, billingdomain.CreditAccountParams{
				AccountID: account.AccountID, Amount: sub.AmountTotal - sub.AmountUsed,
				IdempotencyKey: fmt.Sprintf("test-subscription-bootstrap:%d", sub.Id),
				ReasonCode:     "test_subscription_bootstrap",
			})
			return err
		}))
	}

	order := &commerceschema.GroupBuyOrder{
		Id:           98201,
		InitiatorId:  users[0].Id,
		PlanId:       plan.Id,
		Status:       commerceschema.GroupBuyStatusPending,
		TargetCount:  5,
		CurrentCount: 2,
		ExpiresAt:    now.Add(-time.Minute).Unix(),
	}
	require.NoError(t, db.Create(order).Error)

	members := []*commerceschema.GroupBuyMember{
		{Id: 98301, GroupBuyId: order.Id, UserId: users[0].Id, OrderId: 99101, UserSubscriptionId: subs[0].Id},
		{Id: 98302, GroupBuyId: order.Id, UserId: users[1].Id, OrderId: 99102, UserSubscriptionId: subs[1].Id},
	}
	for _, member := range members {
		require.NoError(t, db.Create(member).Error)
	}

	require.NoError(t, settleGroupBuyOrder(order.Id))

	expectedBonusQuota := quotaUnitsFromUSD(plan.GroupBuyBonus2)

	var refreshedSubs []commerceschema.UserSubscription
	require.NoError(t, db.Where("id IN ?", []int{subs[0].Id, subs[1].Id}).Order("id asc").Find(&refreshedSubs).Error)
	require.Len(t, refreshedSubs, 2)
	for _, sub := range refreshedSubs {
		assert.Equal(t, plan.TotalAmount+expectedBonusQuota, sub.AmountTotal)
		assert.Equal(t, plan.PeriodAmount+expectedBonusQuota, sub.PeriodAmount)

		var account billingschema.BillingAccount
		require.NoError(t, db.Where("owner_type = ? AND owner_id = ? AND account_type = ?", "user_subscription", sub.Id, "subscription").First(&account).Error)
		var snapshot billingschema.BillingBalanceSnapshot
		require.NoError(t, db.Where("account_id = ?", account.AccountID).First(&snapshot).Error)
		assert.Equal(t, sub.AmountTotal-sub.AmountUsed, snapshot.AvailableBalance)
	}

	var refreshedUsers []identityschema.User
	require.NoError(t, db.Where("id IN ?", []int{users[0].Id, users[1].Id}).Order("id asc").Find(&refreshedUsers).Error)
	require.Len(t, refreshedUsers, 2)
	assert.Equal(t, 777, refreshedUsers[0].Quota)
	assert.Equal(t, 888, refreshedUsers[1].Quota)

	var refreshedMembers []commerceschema.GroupBuyMember
	require.NoError(t, db.Where("group_buy_id = ?", order.Id).Order("id asc").Find(&refreshedMembers).Error)
	require.Len(t, refreshedMembers, 2)
	for _, member := range refreshedMembers {
		assert.True(t, member.BonusGranted)
		assert.Equal(t, plan.GroupBuyBonus2, member.BonusAmountUSD)
	}

	var refreshedOrder commerceschema.GroupBuyOrder
	require.NoError(t, db.Where("id = ?", order.Id).First(&refreshedOrder).Error)
	assert.Equal(t, commerceschema.GroupBuyStatusCompleted, refreshedOrder.Status)
	assert.NotZero(t, refreshedOrder.SettledAt)
}

func TestSettleGroupBuyOrder_OnlyOneRealMember_MarkedExpired(t *testing.T) {
	db := setupRedemptionTestDB(t)

	plan := &commerceschema.SubscriptionPlan{
		Id:               9911,
		Title:            "Test月卡",
		PriceAmount:      99,
		Currency:         "CNY",
		DurationUnit:     commerceschema.SubscriptionDurationMonth,
		DurationValue:    1,
		Enabled:          true,
		GroupBuyEnabled:  true,
		GroupBuyBonus2:   20,
		GroupBuyBonus3:   35,
		GroupBuyBonus5:   50,
		TotalAmount:      int64(platformruntime.QuotaPerUnit * 100),
		PeriodAmount:     int64(platformruntime.QuotaPerUnit * 40),
		QuotaResetPeriod: commerceschema.SubscriptionResetMonthly,
	}
	require.NoError(t, db.Create(plan).Error)

	realUser := &identityschema.User{
		Id:       9912,
		Username: "real_user_solo",
		Status:   constant.UserStatusEnabled,
		Quota:    1000,
		AffCode:  "RU9912",
	}
	require.NoError(t, db.Create(realUser).Error)

	now := time.Now()
	sub := &commerceschema.UserSubscription{
		Id:           9913,
		UserId:       realUser.Id,
		PlanId:       plan.Id,
		AmountTotal:  plan.TotalAmount,
		AmountUsed:   0,
		PeriodAmount: plan.PeriodAmount,
		PeriodUsed:   0,
		StartTime:    now.Unix(),
		EndTime:      now.Add(30 * 24 * time.Hour).Unix(),
		Status:       "active",
	}
	require.NoError(t, db.Create(sub).Error)

	order := &commerceschema.GroupBuyOrder{
		Id:           9914,
		InitiatorId:  realUser.Id,
		PlanId:       plan.Id,
		Status:       commerceschema.GroupBuyStatusPending,
		TargetCount:  5,
		CurrentCount: 2,
		ExpiresAt:    now.Add(-time.Minute).Unix(),
	}
	require.NoError(t, db.Create(order).Error)

	members := []*commerceschema.GroupBuyMember{
		{
			GroupBuyId:         order.Id,
			UserId:             realUser.Id,
			OrderId:            1,
			UserSubscriptionId: sub.Id,
			BonusGranted:       false,
		},
		{
			GroupBuyId:   order.Id,
			UserId:       19913,
			OrderId:      0,
			BonusGranted: true,
		},
	}
	for _, member := range members {
		require.NoError(t, db.Create(member).Error)
	}

	initialTotal := sub.AmountTotal
	require.NoError(t, settleGroupBuyOrder(order.Id))

	var settled commerceschema.GroupBuyOrder
	require.NoError(t, db.Where("id = ?", order.Id).First(&settled).Error)
	assert.Equal(t, commerceschema.GroupBuyStatusExpired, settled.Status)

	var updatedSub commerceschema.UserSubscription
	require.NoError(t, db.Where("id = ?", sub.Id).First(&updatedSub).Error)
	assert.Equal(t, initialTotal, updatedSub.AmountTotal)
}

func TestSettleGroupBuyOrder_TwoRealMembers_MarkedCompleted(t *testing.T) {
	db := setupRedemptionTestDB(t)

	plan := &commerceschema.SubscriptionPlan{
		Id:               9921,
		Title:            "Test月卡",
		PriceAmount:      99,
		Currency:         "CNY",
		DurationUnit:     commerceschema.SubscriptionDurationMonth,
		DurationValue:    1,
		Enabled:          true,
		GroupBuyEnabled:  true,
		GroupBuyBonus2:   20,
		GroupBuyBonus3:   35,
		GroupBuyBonus5:   50,
		TotalAmount:      int64(platformruntime.QuotaPerUnit * 100),
		PeriodAmount:     int64(platformruntime.QuotaPerUnit * 40),
		QuotaResetPeriod: commerceschema.SubscriptionResetMonthly,
	}
	require.NoError(t, db.Create(plan).Error)

	users := []*identityschema.User{
		{Id: 9922, Username: "real_user_1", Status: constant.UserStatusEnabled, Quota: 1000, AffCode: "RU9922"},
		{Id: 9923, Username: "real_user_2", Status: constant.UserStatusEnabled, Quota: 1000, AffCode: "RU9923"},
	}
	for _, user := range users {
		require.NoError(t, db.Create(user).Error)
	}

	now := time.Now()
	subs := []*commerceschema.UserSubscription{
		{
			Id:           9924,
			UserId:       users[0].Id,
			PlanId:       plan.Id,
			AmountTotal:  plan.TotalAmount,
			AmountUsed:   0,
			PeriodAmount: plan.PeriodAmount,
			PeriodUsed:   0,
			StartTime:    now.Unix(),
			EndTime:      now.Add(30 * 24 * time.Hour).Unix(),
			Status:       "active",
		},
		{
			Id:           9925,
			UserId:       users[1].Id,
			PlanId:       plan.Id,
			AmountTotal:  plan.TotalAmount,
			AmountUsed:   0,
			PeriodAmount: plan.PeriodAmount,
			PeriodUsed:   0,
			StartTime:    now.Unix(),
			EndTime:      now.Add(30 * 24 * time.Hour).Unix(),
			Status:       "active",
		},
	}
	for _, sub := range subs {
		require.NoError(t, db.Create(sub).Error)
	}

	order := &commerceschema.GroupBuyOrder{
		Id:           9926,
		InitiatorId:  users[0].Id,
		PlanId:       plan.Id,
		Status:       commerceschema.GroupBuyStatusPending,
		TargetCount:  5,
		CurrentCount: 3,
		ExpiresAt:    now.Add(-time.Minute).Unix(),
	}
	require.NoError(t, db.Create(order).Error)

	members := []*commerceschema.GroupBuyMember{
		{GroupBuyId: order.Id, UserId: users[0].Id, OrderId: 1, UserSubscriptionId: subs[0].Id, BonusGranted: false},
		{GroupBuyId: order.Id, UserId: users[1].Id, OrderId: 2, UserSubscriptionId: subs[1].Id, BonusGranted: false},
		{GroupBuyId: order.Id, UserId: 19926, OrderId: 0, BonusGranted: true},
	}
	for _, member := range members {
		require.NoError(t, db.Create(member).Error)
	}

	initialTotal := subs[0].AmountTotal
	require.NoError(t, settleGroupBuyOrder(order.Id))

	var settled commerceschema.GroupBuyOrder
	require.NoError(t, db.Where("id = ?", order.Id).First(&settled).Error)
	assert.Equal(t, commerceschema.GroupBuyStatusCompleted, settled.Status)

	expectedBonus := int64(platformruntime.QuotaPerUnit * 20)
	for _, sub := range subs {
		var updated commerceschema.UserSubscription
		require.NoError(t, db.Where("id = ?", sub.Id).First(&updated).Error)
		assert.Equal(t, initialTotal+expectedBonus, updated.AmountTotal)
	}
}
