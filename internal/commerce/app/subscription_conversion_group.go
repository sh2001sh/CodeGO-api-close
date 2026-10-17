package app

import (
	"github.com/sh2001sh/new-api/constant"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type monthlyPassConversionGroup struct {
	BonusPercent int   `json:"bonus_percent"`
	GroupBuyID   int64 `json:"group_buy_id"`
	MemberCount  int   `json:"group_buy_member_count"`
}

func applyMonthlyPassGroupBonus(quote *monthlyPassConversionQuote, group monthlyPassConversionGroup) {
	quote.bonusQuota = int(decimal.NewFromInt(int64(quote.baseQuota)).Mul(decimal.NewFromInt(int64(group.BonusPercent))).Div(decimal.NewFromInt(100)).Floor().IntPart())
	quote.targetQuota = quote.baseQuota + quote.bonusQuota
}

// Only the room tied to this exact legacy subscription can supply its earned
// tier. Synthetic members and unsuccessful, refunded, or unrelated payments
// never increase the wallet compensation.
func monthlyPassConversionGroupTx(tx *gorm.DB, sub *commerceschema.UserSubscription) (monthlyPassConversionGroup, error) {
	result := monthlyPassConversionGroup{}
	if sub == nil {
		return result, nil
	}
	// One statement observes a consistent payment/membership snapshot. Read-only
	// joins avoid the order/subscription lock inversion used by cash refunds.
	var members []commerceschema.GroupBuyMember
	ownRooms := tx.Model(&commerceschema.GroupBuyMember{}).Select("group_buy_id").Where("user_subscription_id = ? AND user_id = ? AND order_id > 0", sub.Id, sub.UserId)
	err := tx.Table("group_buy_members AS gm").Select("gm.*").
		Joins("JOIN group_buy_orders AS room ON room.id = gm.group_buy_id AND room.plan_id = ? AND room.status IN ?", sub.PlanId, []string{commerceschema.GroupBuyStatusPending, commerceschema.GroupBuyStatusCompleted, commerceschema.GroupBuyStatusExpired}).
		Joins("JOIN subscription_orders AS paid ON paid.id = gm.order_id AND paid.group_buy_id = gm.group_buy_id AND paid.user_id = gm.user_id AND paid.target_subscription_id = gm.user_subscription_id AND paid.plan_id = room.plan_id").
		Joins("JOIN users AS member_user ON member_user.id = gm.user_id AND member_user."+subscriptionUserGroupColumn()+" <> ?", "ghost").
		Where("gm.group_buy_id IN (?) AND gm.order_id > 0 AND gm.user_subscription_id > 0 AND paid.status = ? AND paid.money > 0 AND ((paid.refund_status = ? AND paid.refund_amount = 0) OR paid.refund_status = ?)", ownRooms, constant.TopUpStatusSuccess, "", commerceschema.RefundStatusFailed).
		Find(&members).Error
	if err != nil {
		return result, err
	}
	validByRoom := make(map[int64]map[int]struct{})
	ownValid := make(map[int64]bool)
	for _, member := range members {
		if validByRoom[member.GroupBuyId] == nil {
			validByRoom[member.GroupBuyId] = make(map[int]struct{})
		}
		validByRoom[member.GroupBuyId][member.UserId] = struct{}{}
		if member.UserId == sub.UserId && member.UserSubscriptionId == sub.Id {
			ownValid[member.GroupBuyId] = true
		}
	}
	for roomID, valid := range validByRoom {
		if !ownValid[roomID] {
			continue
		}
		percent := 0
		if len(valid) >= 5 {
			percent = 5
		} else if len(valid) >= 3 {
			percent = 3
		}
		if percent > result.BonusPercent {
			result = monthlyPassConversionGroup{BonusPercent: percent, GroupBuyID: roomID, MemberCount: len(valid)}
		}
	}
	return result, nil
}
