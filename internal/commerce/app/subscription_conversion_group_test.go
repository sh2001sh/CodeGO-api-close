package app

import (
	"fmt"
	"testing"

	"github.com/sh2001sh/new-api/constant"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"github.com/stretchr/testify/require"
)

func TestConversionGroupUsesEarnedRealPaidTierAndFreezesReceipt(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		count   int
		percent int
		invalid string
	}{
		{name: "two", count: 2}, {name: "three", count: 3, percent: 3},
		{name: "four", count: 4, percent: 3}, {name: "five", count: 5, percent: 5},
		{name: "ghost excluded", count: 3, invalid: "ghost"},
		{name: "failed payment excluded", count: 3, invalid: "failed"},
		{name: "refunded payment excluded", count: 3, invalid: "refunded"},
		{name: "refund pending excluded", count: 3, invalid: "refunding"},
		{name: "partial refund excluded", count: 3, invalid: "partial"},
		{name: "failed refund remains paid", count: 3, percent: 3, invalid: "refund_failed"},
		{name: "soft deleted historical paid member counts", count: 3, percent: 3, invalid: "deleted"},
		{name: "five with two refunded", count: 5, percent: 3, invalid: "two_refunded"},
		{name: "unbound legacy member excluded", count: 3, invalid: "unbound"},
		{name: "wrong ownership excluded", count: 3, invalid: "owner"},
		{name: "wrong target excluded", count: 3, invalid: "target"},
		{name: "pending room three paid", count: 3, percent: 3, invalid: "pending"},
		{name: "pending room four paid", count: 4, percent: 3, invalid: "pending"},
		{name: "expired room five paid", count: 5, percent: 5, invalid: "expired"},
		{name: "unknown room state excluded", count: 5, invalid: "unknown"},
		{name: "other room excluded", count: 3, invalid: "room"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			db, user, plan, sub := setupMonthlyPassConversionTest(t, 75001, 75002, 75003, 299, 50)
			room := commerceschema.GroupBuyOrder{PlanId: plan.Id, InitiatorId: user.Id, Status: commerceschema.GroupBuyStatusCompleted, CurrentCount: 99}
			if scenario.invalid == "pending" {
				room.Status = commerceschema.GroupBuyStatusPending
			}
			if scenario.invalid == "expired" {
				room.Status = commerceschema.GroupBuyStatusExpired
			}
			if scenario.invalid == "unknown" {
				room.Status = "invalid"
			}
			require.NoError(t, db.Create(&room).Error)
			for i := range scenario.count {
				memberUser, memberSub := user.Id, sub.Id
				if i > 0 {
					memberUser, memberSub = 76000+i, 77000+i
					member := identityschema.User{Id: memberUser, Username: fmt.Sprintf("group-paid-%d", i), AffCode: fmt.Sprintf("paid%d", i), Group: "default"}
					if i == scenario.count-1 && scenario.invalid == "ghost" {
						member.Group = "ghost"
					}
					require.NoError(t, db.Create(&member).Error)
					if i == scenario.count-1 && scenario.invalid == "deleted" {
						require.NoError(t, db.Delete(&member).Error)
					}
				}
				order := commerceschema.SubscriptionOrder{UserId: memberUser, PlanId: plan.Id, TargetSubscriptionId: memberSub, GroupBuyId: room.Id, Money: 299, TradeNo: fmt.Sprintf("group-order-%d", i), Status: constant.TopUpStatusSuccess}
				if i == scenario.count-1 {
					switch scenario.invalid {
					case "failed":
						order.Status = "failed"
					case "owner":
						order.UserId = 99999
					case "target":
						order.TargetSubscriptionId = 99999
					case "room":
						order.GroupBuyId = 99999
					case "refunded":
						order.RefundStatus = commerceschema.RefundStatusSuccess
					case "refunding":
						order.RefundStatus = commerceschema.RefundStatusProcessing
					case "partial":
						order.RefundAmount = 1
					case "refund_failed":
						order.RefundStatus = commerceschema.RefundStatusFailed
						order.RefundAmount = 299
					case "unbound":
						memberSub = 0
					}
				}
				if scenario.invalid == "two_refunded" && i >= 3 {
					order.RefundStatus = commerceschema.RefundStatusSuccess
				}
				require.NoError(t, db.Create(&order).Error)
				require.NoError(t, db.Create(&commerceschema.GroupBuyMember{GroupBuyId: room.Id, UserId: memberUser, UserSubscriptionId: memberSub, OrderId: order.Id}).Error)
			}
			preview := BuildSubscriptionClaudeConversionPreview(plan, sub)
			require.True(t, preview.Eligible)
			require.Equal(t, scenario.percent, preview.BonusPercent)
			result, err := ConvertMonthlyPassToUnifiedCredit("group-conversion", user.Id, sub.Id, 50)
			require.NoError(t, err)
			require.Equal(t, scenario.percent, result.BonusPercent)
			require.EqualValues(t, quotaUnitsFromUSD(149.5), result.BaseQuota)
			require.EqualValues(t, int64(result.BaseQuota)*int64(scenario.percent)/100, result.BonusQuota)
			// A later room/payment change cannot retroactively alter a receipt.
			require.NoError(t, db.Model(&room).Update("status", "expired").Error)
			replay, err := ConvertMonthlyPassToUnifiedCredit("group-conversion", user.Id, sub.Id, 50)
			require.NoError(t, err)
			require.Equal(t, result.BonusQuota, replay.BonusQuota)
			require.Equal(t, result.BonusPercent, replay.BonusPercent)
			require.Equal(t, result.GroupBuyID, replay.GroupBuyID)
		})
	}
}
