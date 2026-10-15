package app

import (
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

// GroupBuyReconciliationResult describes a settled monthly group-buy reconciliation run.
type GroupBuyReconciliationResult struct {
	GroupsScanned    int
	MembersAdjusted  int
	EligibleBonusUSD float64
}

func settledMonthlyGroupBuyIDs() ([]int64, error) {
	var groupBuyIDs []int64
	err := platformdb.DB.Model(&commerceschema.GroupBuyOrder{}).
		Joins("JOIN subscription_plans ON subscription_plans.id = group_buy_orders.plan_id").
		Where("group_buy_orders.status IN ?", []string{
			commerceschema.GroupBuyStatusCompleted,
			commerceschema.GroupBuyStatusExpired,
		}).
		Where("subscription_plans.duration_unit = ?", commerceschema.SubscriptionDurationMonth).
		Order("group_buy_orders.id ASC").
		Pluck("group_buy_orders.id", &groupBuyIDs).Error
	return groupBuyIDs, err
}

// InspectSettledMonthlyGroupBuyBonuses reports missing tier differences for
// active monthly subscriptions without changing a subscription ledger.
func InspectSettledMonthlyGroupBuyBonuses() (GroupBuyReconciliationResult, error) {
	result := GroupBuyReconciliationResult{}
	groupBuyIDs, err := settledMonthlyGroupBuyIDs()
	if err != nil {
		return result, err
	}
	for _, groupBuyID := range groupBuyIDs {
		if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
			var order commerceschema.GroupBuyOrder
			if err := tx.First(&order, groupBuyID).Error; err != nil {
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
				sub, err := getGroupBuyMemberSubscriptionTx(tx, member, order.PlanId)
				if err != nil {
					return err
				}
				if sub.Status != "active" {
					continue
				}
				result.MembersAdjusted++
				result.EligibleBonusUSD += target - member.BonusAmountUSD
			}
			return nil
		}); err != nil {
			return result, err
		}
		result.GroupsScanned++
	}
	return result, nil
}

// ReconcileSettledMonthlyGroupBuyBonuses applies missing tier differences only
// to active monthly subscriptions in settled group buys. Pending groups and
// expired subscription history must settle or remain untouched respectively.
func ReconcileSettledMonthlyGroupBuyBonuses() (GroupBuyReconciliationResult, error) {
	result, err := InspectSettledMonthlyGroupBuyBonuses()
	if err != nil || result.MembersAdjusted == 0 {
		return result, err
	}
	groupBuyIDs, err := settledMonthlyGroupBuyIDs()
	if err != nil {
		return result, err
	}
	result.MembersAdjusted = 0

	for _, groupBuyID := range groupBuyIDs {
		adjusted, err := reconcileGroupBuyBonus(groupBuyID, true)
		if err != nil {
			return result, err
		}
		result.MembersAdjusted += adjusted
	}
	return result, nil
}
