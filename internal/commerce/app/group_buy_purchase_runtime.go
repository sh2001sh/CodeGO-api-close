package app

import (
	"errors"
	"fmt"
	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
	"time"
)

// ValidateGroupBuyPurchase validates the requested group-buy purchase mode before payment creation.
func ValidateGroupBuyPurchase(userID int, planID int, purchaseType string, groupBuyID int64) error {
	purchaseType = commercedomain.NormalizeSubscriptionPurchaseType(purchaseType)
	if purchaseType == commerceschema.SubscriptionPurchaseTypeNormal {
		return nil
	}

	plan, err := GetSubscriptionPlanByID(planID)
	if err != nil {
		return err
	}
	if !supportsGroupBuyPlan(plan) {
		return ErrGroupBuyPlanNotEnabled
	}

	if purchaseType == commerceschema.SubscriptionPurchaseTypeGroupBuy {
		order, err := findJoinableGroupBuyByPlanTx(nil, planID)
		if err != nil || order == nil {
			return err
		}
		return ensureUserCanJoinGroupBuyTx(nil, order.Id, userID)
	}

	if groupBuyID <= 0 {
		return ErrGroupBuyNotFound
	}
	order, err := getJoinableGroupBuyTx(nil, groupBuyID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrGroupBuyNotFound
		}
		return err
	}
	if order.PlanId != planID {
		return ErrGroupBuyNotJoinable
	}
	return ensureUserCanJoinGroupBuyTx(nil, groupBuyID, userID)
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
	if !supportsGroupBuyPlan(plan) {
		return ErrGroupBuyPlanNotEnabled
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
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("id = ?", groupBuyID).First(&order).Error; err != nil {
			return err
		}
		if order.Status != commerceschema.GroupBuyStatusPending {
			return nil
		}

		var members []commerceschema.GroupBuyMember
		if err := tx.Where("group_buy_id = ?", order.Id).Find(&members).Error; err != nil {
			return err
		}
		plan, err := getSubscriptionPlanRecordTx(tx, order.PlanId)
		if err != nil {
			return err
		}

		memberCount := len(members)
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
				if err != nil {
					return err
				}
				if err := addSubscriptionBonusTx(tx, sub, int64(quota)); err != nil {
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
	adjusted := 0
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var order commerceschema.GroupBuyOrder
		if err := tx.Set("gorm:query_option", "FOR UPDATE").First(&order, groupBuyID).Error; err != nil {
			return err
		}
		var members []commerceschema.GroupBuyMember
		if err := tx.Where("group_buy_id = ?", groupBuyID).Find(&members).Error; err != nil {
			return err
		}
		plan, err := getSubscriptionPlanRecordTx(tx, order.PlanId)
		if err != nil {
			return err
		}
		target := bonusForGroupBuyCount(*plan, len(members))
		for _, member := range members {
			if member.OrderId == 0 || target <= member.BonusAmountUSD {
				continue
			}
			delta := target - member.BonusAmountUSD
			sub, err := getGroupBuyMemberSubscriptionTx(tx, member, order.PlanId)
			if err != nil {
				return err
			}
			if err := addSubscriptionBonusTx(tx, sub, int64(quotaUnitsFromUSD(delta))); err != nil {
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
	query := tx.Set("gorm:query_option", "FOR UPDATE")
	if member.UserSubscriptionId > 0 {
		err := query.Where("id = ? AND user_id = ?", member.UserSubscriptionId, member.UserId).First(&sub).Error
		if err == nil {
			return &sub, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}

	err := query.Where("user_id = ? AND plan_id = ? AND status = ?", member.UserId, planID, "active").
		Order("created_at desc, id desc").
		First(&sub).Error
	if err != nil {
		return nil, err
	}
	return &sub, nil
}
