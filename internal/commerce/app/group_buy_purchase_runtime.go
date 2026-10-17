package app

import (
	"errors"
	"fmt"
	"math"

	"github.com/sh2001sh/new-api/constant"
	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

// ValidateGroupBuyPurchase validates the requested group-buy purchase mode before payment creation.
func ValidateGroupBuyPurchase(userID int, planID int, purchaseType string, groupBuyID int64) error {
	purchaseType = commercedomain.NormalizeSubscriptionPurchaseType(purchaseType)
	if purchaseType == commerceschema.SubscriptionPurchaseTypeNormal {
		return nil
	}

	return ErrGroupBuyPlanNotEnabled
}

// ApplyGroupBuyPurchaseAfterPaymentTx applies group-buy enrollment after a paid subscription order is granted.
func ApplyGroupBuyPurchaseAfterPaymentTx(tx *gorm.DB, order *commerceschema.SubscriptionOrder, plan *commerceschema.SubscriptionPlan, sub *commerceschema.UserSubscription) error {
	if tx == nil || order == nil || plan == nil {
		return errors.New("invalid group buy purchase args")
	}

	purchaseType := commercedomain.NormalizeSubscriptionPurchaseType(order.PurchaseType)
	if purchaseType == commerceschema.SubscriptionPurchaseTypeNormal {
		return nil
	}

	if order.Id <= 0 || order.Status != constant.TopUpStatusSuccess || order.Money <= 0 || math.IsNaN(order.Money) || math.IsInf(order.Money, 0) ||
		sub == nil || sub.Id <= 0 || sub.UserId != order.UserId || sub.PlanId != plan.Id || order.PlanId != plan.Id ||
		(order.RefundStatus != "" && order.RefundStatus != commerceschema.RefundStatusFailed) {
		return ErrGroupBuyNotJoinable
	}

	if purchaseType == commerceschema.SubscriptionPurchaseTypeGroupBuy {
		if err := lockGroupBuyPlanTx(tx, plan.Id); err != nil {
			return err
		}
		existingOrder, err := findJoinableGroupBuyByPlanTx(tx, plan.Id)
		if err != nil {
			return err
		}
		if existingOrder != nil {
			order.GroupBuyId = existingOrder.Id
			return joinGroupBuyTx(tx, order.UserId, existingOrder.Id, order.Id, plan.Id, groupBuySubscriptionID(sub))
		}

		groupOrder := &commerceschema.GroupBuyOrder{
			InitiatorId:  order.UserId,
			PlanId:       plan.Id,
			Status:       commerceschema.GroupBuyStatusPending,
			TargetCount:  5,
			CurrentCount: 1,
			ExpiresAt:    time.Now().Add(48 * time.Hour).Unix(),
		}
		if err := tx.Create(groupOrder).Error; err != nil {
			return err
		}

		member := commerceschema.GroupBuyMember{
			GroupBuyId:         groupOrder.Id,
			UserId:             order.UserId,
			OrderId:            order.Id,
			UserSubscriptionId: groupBuySubscriptionID(sub),
		}
		if err := tx.Create(&member).Error; err != nil {
			return err
		}
		order.GroupBuyId = groupOrder.Id
		return nil
	}

	if purchaseType == commerceschema.SubscriptionPurchaseTypeJoinGroup {
		return joinGroupBuyTx(tx, order.UserId, order.GroupBuyId, order.Id, plan.Id, groupBuySubscriptionID(sub))
	}
	return nil
}

func groupBuySubscriptionID(sub *commerceschema.UserSubscription) int {
	if sub == nil {
		return 0
	}
	return sub.Id
}

func bonusForGroupBuyCount(plan commerceschema.SubscriptionPlan, count int) float64 {
	switch {
	case count >= 5:
		return plan.GroupBuyBonus5
	case count >= 3:
		return plan.GroupBuyBonus3
	case count >= 2:
		return plan.GroupBuyBonus2
	default:
		return 0
	}
}

func settleGroupBuyOrder(groupBuyID int64) error {
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var order commerceschema.GroupBuyOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", groupBuyID).First(&order).Error; err != nil {
			return err
		}
		if order.Status != commerceschema.GroupBuyStatusPending {
			return nil
		}

		members, memberCount, err := qualifiedGroupBuyMembersTx(tx, &order, true)
		if err != nil {
			return err
		}
		plan, err := getSubscriptionPlanRecordTx(tx, order.PlanId)
		if err != nil {
			return err
		}

		bonusUSD := bonusForGroupBuyCount(*plan, memberCount)
		status := commerceschema.GroupBuyStatusExpired
		if memberCount >= 2 {
			status = commerceschema.GroupBuyStatusCompleted
		}
		if bonusUSD > 0 {
			quota := int(quotaUnitsFromUSD(bonusUSD))
			for _, member := range members {
				if member.BonusGranted {
					continue
				}
				sub, err := getGroupBuyMemberSubscriptionTx(tx, member, order.PlanId)
				if errors.Is(err, gorm.ErrRecordNotFound) {
					platformobservability.SysLog(fmt.Sprintf("group buy settlement skipped missing subscription group_id=%d member_id=%d", order.Id, member.Id))
					continue
				}
				if err != nil {
					return err
				}
				if sub.Status != "active" {
					continue
				}
				grantKey := fmt.Sprintf("group-buy:%d:member:%d:tier:%d", order.Id, member.Id, quota)
				if err := addSubscriptionBonusTx(tx, sub, int64(quota), grantKey); err != nil {
					return err
				}
				if err := tx.Model(&commerceschema.GroupBuyMember{}).Where("id = ?", member.Id).
					Updates(map[string]any{
						"bonus_granted":    true,
						"bonus_amount_usd": bonusUSD,
					}).Error; err != nil {
					return err
				}
				if err := auditapp.RecordLogTx(tx, member.UserId, auditschema.LogTypeTopup, fmt.Sprintf("集享计划加成到账，已加入套餐额度，套餐: %s，加成额度: $%.2f", plan.Title, bonusUSD)); err != nil {
					return err
				}
			}
		}

		return tx.Model(&commerceschema.GroupBuyOrder{}).Where("id = ?", order.Id).
			Updates(map[string]any{
				"status":     status,
				"settled_at": platformruntime.GetTimestamp(),
				"updated_at": platformruntime.GetTimestamp(),
			}).Error
	})
}

// ReconcileGroupBuyBonus applies missing tier differences to real members only.
func ReconcileGroupBuyBonus(groupBuyID int64) (int, error) {
	return reconcileGroupBuyBonus(groupBuyID, false)
}

func reconcileGroupBuyBonus(groupBuyID int64, activeSubscriptionsOnly bool) (int, error) {
	adjusted := 0
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var order commerceschema.GroupBuyOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, groupBuyID).Error; err != nil {
			return err
		}
		members, memberCount, err := qualifiedGroupBuyMembersTx(tx, &order, true)
		if err != nil {
			return err
		}
		plan, err := getSubscriptionPlanRecordTx(tx, order.PlanId)
		if err != nil {
			return err
		}
		target := bonusForGroupBuyCount(*plan, memberCount)
		for _, member := range members {
			if member.OrderId == 0 || target <= member.BonusAmountUSD {
				continue
			}
			delta := target - member.BonusAmountUSD
			sub, err := getGroupBuyMemberSubscriptionTx(tx, member, order.PlanId)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if sub.Status == "refund_pending" || sub.Status == "cancelled" || sub.Status == commerceschema.SubscriptionStatusSettledCancelled || (activeSubscriptionsOnly && sub.Status != "active") {
				continue
			}
			targetQuota := quotaUnitsFromUSD(target)
			grantKey := fmt.Sprintf("group-buy:%d:member:%d:tier:%d", order.Id, member.Id, targetQuota)
			if err := addSubscriptionBonusTx(tx, sub, int64(quotaUnitsFromUSD(delta)), grantKey); err != nil {
				return err
			}
			if err := tx.Model(&commerceschema.GroupBuyMember{}).Where("id = ?", member.Id).Updates(map[string]any{"bonus_granted": true, "bonus_amount_usd": target}).Error; err != nil {
				return err
			}
			if err := auditapp.RecordLogTx(tx, member.UserId, auditschema.LogTypeTopup, fmt.Sprintf("集享计划档位补差到账，套餐: %s，加成额度: $%.2f", plan.Title, delta)); err != nil {
				return err
			}
			adjusted++
		}
		return nil
	})
	return adjusted, err
}

func getGroupBuyMemberSubscriptionTx(tx *gorm.DB, member commerceschema.GroupBuyMember, planID int) (*commerceschema.UserSubscription, error) {
	if tx == nil {
		return nil, errors.New("tx is nil")
	}

	var sub commerceschema.UserSubscription
	if member.UserSubscriptionId <= 0 {
		return nil, gorm.ErrRecordNotFound
	}
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND user_id = ? AND plan_id = ?", member.UserSubscriptionId, member.UserId, planID).First(&sub).Error; err != nil {
		return nil, err
	}
	return &sub, nil
}

// Historical tiers retain their original synthetic participants. Only an exact
// successful payment can qualify a real participant or receive a new grant.
// Lock payment rows before subscriptions, matching cash refund lock order.
func qualifiedGroupBuyMembersTx(tx *gorm.DB, room *commerceschema.GroupBuyOrder, lock bool) ([]commerceschema.GroupBuyMember, int, error) {
	var members []commerceschema.GroupBuyMember
	if err := tx.Where("group_buy_id = ?", room.Id).Order("id asc").Find(&members).Error; err != nil {
		return nil, 0, err
	}
	qualified := make([]commerceschema.GroupBuyMember, 0, len(members))
	tierCount := 0
	for _, member := range members {
		if member.OrderId <= 0 {
			var ghosts int64
			if err := tx.Table("users").Where("id = ? AND "+subscriptionUserGroupColumn()+" = ?", member.UserId, "ghost").Count(&ghosts).Error; err != nil {
				return nil, 0, err
			}
			if ghosts == 1 && member.BonusGranted {
				tierCount++
			}
			continue
		}
		query := tx
		if lock {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		var paid commerceschema.SubscriptionOrder
		if err := query.Where("id = ? AND user_id = ? AND group_buy_id = ? AND plan_id = ?", member.OrderId, member.UserId, room.Id, room.PlanId).First(&paid).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return nil, 0, err
		}
		if paid.Status != constant.TopUpStatusSuccess || paid.FulfillmentStatus != commerceschema.SubscriptionOrderFulfillmentCompleted || paid.Money <= 0 || math.IsNaN(paid.Money) || math.IsInf(paid.Money, 0) || paid.TargetSubscriptionId <= 0 ||
			(paid.RefundStatus != "" && paid.RefundStatus != commerceschema.RefundStatusFailed) ||
			(paid.RefundStatus == "" && paid.RefundAmount != 0) ||
			(member.UserSubscriptionId > 0 && member.UserSubscriptionId != paid.TargetSubscriptionId) {
			continue
		}
		member.UserSubscriptionId = paid.TargetSubscriptionId
		tierCount++
		qualified = append(qualified, member)
	}
	return qualified, tierCount, nil
}
