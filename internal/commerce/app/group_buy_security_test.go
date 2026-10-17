package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/constant"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedQualifiedGroupBuy(t *testing.T, db *gorm.DB, stamp int) (*commerceschema.GroupBuyOrder, *commerceschema.SubscriptionOrder, *commerceschema.UserSubscription) {
	t.Helper()
	plan := commerceschema.SubscriptionPlan{Title: "legacy group", TotalAmount: 1000, GroupBuyBonus2: 1}
	require.NoError(t, db.Create(&plan).Error)
	user := identityschema.User{Username: fmt.Sprintf("group-paid-%d", stamp), AffCode: fmt.Sprintf("group-paid-%d", stamp), Group: "default", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	ghost := identityschema.User{Username: fmt.Sprintf("group-ghost-%d", stamp), AffCode: fmt.Sprintf("group-ghost-%d", stamp), Group: "ghost"}
	require.NoError(t, db.Create(&ghost).Error)
	sub := commerceschema.UserSubscription{UserId: user.Id, PlanId: plan.Id, AmountTotal: 1000, Status: "active", StartTime: time.Now().Add(-time.Hour).Unix(), EndTime: time.Now().Add(time.Hour).Unix()}
	require.NoError(t, db.Create(&sub).Error)
	room := commerceschema.GroupBuyOrder{InitiatorId: user.Id, PlanId: plan.Id, CurrentCount: 2, ExpiresAt: time.Now().Add(-time.Minute).Unix()}
	require.NoError(t, db.Create(&room).Error)
	paid := commerceschema.SubscriptionOrder{UserId: user.Id, PlanId: plan.Id, GroupBuyId: room.Id, TargetSubscriptionId: sub.Id, Money: 99, Status: constant.TopUpStatusSuccess, FulfillmentStatus: commerceschema.SubscriptionOrderFulfillmentCompleted, TradeNo: fmt.Sprintf("group-order-%d", stamp)}
	require.NoError(t, db.Create(&paid).Error)
	require.NoError(t, db.Create(&commerceschema.GroupBuyMember{GroupBuyId: room.Id, UserId: user.Id, OrderId: paid.Id, UserSubscriptionId: sub.Id}).Error)
	require.NoError(t, db.Create(&commerceschema.GroupBuyMember{GroupBuyId: room.Id, UserId: ghost.Id, BonusGranted: true}).Error)
	return &room, &paid, &sub
}

func TestRetiredGroupBuyJoinNeverCreatesParticipants(t *testing.T) {
	db := setupRedemptionTestDB(t)
	for _, orderID := range []int{0, 1, -1} {
		result, err := JoinGroupBuy(1, GroupBuyJoinRequest{GroupBuyId: 1, OrderId: orderID})
		require.ErrorIs(t, err, ErrGroupBuyPlanNotEnabled)
		require.Nil(t, result)
	}
	var count int64
	require.NoError(t, db.Model(&commerceschema.GroupBuyMember{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestGroupBuySettlementRequiresExactPaidOrder(t *testing.T) {
	for _, test := range []struct {
		name    string
		change  map[string]any
		allowed bool
	}{
		{name: "successful payment", allowed: true},
		{name: "failed refund restores paid qualification", change: map[string]any{"refund_status": commerceschema.RefundStatusFailed, "refund_amount": 97.02}, allowed: true},
		{name: "unpaid", change: map[string]any{"status": constant.TopUpStatusPending}},
		{name: "unfulfilled", change: map[string]any{"fulfillment_status": commerceschema.SubscriptionOrderFulfillmentPending}},
		{name: "zero paid", change: map[string]any{"money": 0}},
		{name: "another user", change: map[string]any{"user_id": 9999}},
		{name: "another group", change: map[string]any{"group_buy_id": 9999}},
		{name: "another plan", change: map[string]any{"plan_id": 9999}},
		{name: "another subscription", change: map[string]any{"target_subscription_id": 9999}},
		{name: "processing refund", change: map[string]any{"refund_status": commerceschema.RefundStatusProcessing}},
		{name: "successful refund", change: map[string]any{"refund_status": commerceschema.RefundStatusSuccess}},
		{name: "unexplained refund amount", change: map[string]any{"refund_amount": 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := setupRedemptionTestDB(t)
			room, paid, sub := seedQualifiedGroupBuy(t, db, 1)
			if len(test.change) > 0 {
				require.NoError(t, db.Model(paid).Updates(test.change).Error)
			}
			require.NoError(t, settleGroupBuyOrder(room.Id))
			require.NoError(t, settleGroupBuyOrder(room.Id), "replay must not grant a second bonus")
			var updated commerceschema.UserSubscription
			require.NoError(t, db.First(&updated, sub.Id).Error)
			expected := sub.AmountTotal
			if test.allowed {
				expected += quotaUnitsFromUSD(1)
			}
			require.Equal(t, expected, updated.AmountTotal)
			adjusted, err := ReconcileGroupBuyBonus(room.Id)
			require.NoError(t, err)
			require.Zero(t, adjusted)
		})
	}
}

func TestGroupBuyMissingSubscriptionCannotTargetAnotherActivePass(t *testing.T) {
	db := setupRedemptionTestDB(t)
	room, _, sub := seedQualifiedGroupBuy(t, db, 1)
	require.NoError(t, db.Delete(sub).Error)
	replacement := *sub
	replacement.Id = 0
	require.NoError(t, db.Create(&replacement).Error)
	require.NoError(t, settleGroupBuyOrder(room.Id))
	require.NoError(t, db.First(&replacement, replacement.Id).Error)
	require.EqualValues(t, 1000, replacement.AmountTotal)
	var updated commerceschema.GroupBuyOrder
	require.NoError(t, db.First(&updated, room.Id).Error)
	require.NotEqual(t, commerceschema.GroupBuyStatusPending, updated.Status)
}

func TestGroupBuySettlementSkipsPoisonedBatchesAndReportsFailures(t *testing.T) {
	db := setupRedemptionTestDB(t)
	for i := 0; i < 3; i++ {
		require.NoError(t, db.Create(&commerceschema.GroupBuyOrder{PlanId: 99999, InitiatorId: 1, ExpiresAt: time.Now().Add(-time.Hour).Unix()}).Error)
	}
	room, _, sub := seedQualifiedGroupBuy(t, db, 1)
	settled, err := SettleDueGroupBuys(1)
	require.ErrorContains(t, err, "settle group buy")
	require.Equal(t, 1, settled, "even more than one batch of bad rooms must not starve a good room")
	require.NoError(t, db.First(room, room.Id).Error)
	require.Equal(t, commerceschema.GroupBuyStatusCompleted, room.Status)
	require.NoError(t, db.First(sub, sub.Id).Error)
	require.EqualValues(t, 1000+quotaUnitsFromUSD(1), sub.AmountTotal)
}
