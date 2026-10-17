package app

import (
	"fmt"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"

	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"math/rand"
	"time"

	// SettleDueGroupBuys settles pending group-buy orders that are complete or expired.
	"gorm.io/gorm"
)

func SettleDueGroupBuys(limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}

	now := platformruntime.GetTimestamp()
	settled := 0
	var lastExpiry, lastID int64
	var failure error
	for settled < limit {
		var orders []commerceschema.GroupBuyOrder
		query := platformdb.DB.Where("status = ? AND (expires_at <= ? OR current_count >= target_count)", commerceschema.GroupBuyStatusPending, now)
		if lastID > 0 {
			query = query.Where("expires_at > ? OR (expires_at = ? AND id > ?)", lastExpiry, lastExpiry, lastID)
		}
		batchSize := limit - settled
		if err := query.Order("expires_at asc, id asc").Limit(batchSize).Find(&orders).Error; err != nil {
			return settled, err
		}
		for _, order := range orders {
			lastExpiry, lastID = order.ExpiresAt, order.Id
			if err := settleGroupBuyOrder(order.Id); err != nil {
				failure = fmt.Errorf("settle group buy %d: %w", order.Id, err)
				platformobservability.SysError(failure.Error())
				continue
			}
			settled++
		}
		if len(orders) < batchSize {
			break
		}
	}
	return settled, failure
}

// EnsureGhostGroupBuys ensures enough active ghost group-buy rooms exist for discovery pages.
func EnsureGhostGroupBuys() error {
	if !platformconfig.IsMasterNode {
		return nil
	}
	return ensureGhostGroupBuysInternal()
}

func ensureGhostGroupBuysInternal() error {
	ghostUserIDs, err := initGhostUsersDB()
	if err != nil {
		return err
	}
	if len(ghostUserIDs) == 0 {
		return nil
	}

	now := platformruntime.GetTimestamp()
	var activeCount int64
	if err := platformdb.DB.Model(&commerceschema.GroupBuyOrder{}).
		Where("status = ? AND expires_at > ? AND current_count < target_count", commerceschema.GroupBuyStatusPending, now).
		Count(&activeCount).Error; err != nil {
		return err
	}

	needed := 2 - int(activeCount)
	if needed <= 0 {
		return nil
	}

	var allPlans []commerceschema.SubscriptionPlan
	if err := platformdb.DB.Where("enabled = ? AND internal_only = ? AND group_buy_enabled = ?", true, false, true).
		Find(&allPlans).Error; err != nil {
		return err
	}

	var activePlanIDs []int
	if err := platformdb.DB.Model(&commerceschema.GroupBuyOrder{}).
		Where("status = ? AND expires_at > ? AND current_count < target_count", commerceschema.GroupBuyStatusPending, now).
		Distinct("plan_id").
		Pluck("plan_id", &activePlanIDs).Error; err != nil {
		return err
	}

	activeSet := make(map[int]bool, len(activePlanIDs))
	for _, id := range activePlanIDs {
		activeSet[id] = true
	}

	var candidates []commerceschema.SubscriptionPlan
	for _, plan := range allPlans {
		if !activeSet[plan.Id] {
			candidates = append(candidates, plan)
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	rand.Shuffle(len(candidates), func(i, j int) {
		candidates[i], candidates[j] = candidates[j], candidates[i]
	})
	if needed > len(candidates) {
		needed = len(candidates)
	}

	for _, plan := range candidates[:needed] {
		if err := createGhostGroupBuyOrder(plan, ghostUserIDs); err != nil {
			platformobservability.SysLog(fmt.Sprintf("failed to create ghost group buy for plan %d: %v", plan.Id, err))
		}
	}
	return nil
}

func listGhostUserIDs() ([]int, error) {
	var ids []int
	if err := platformdb.DB.Model(&identityschema.User{}).
		Where(subscriptionUserGroupColumn()+" = ?", "ghost").
		Order("id asc").
		Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

func createGhostGroupBuyOrder(plan commerceschema.SubscriptionPlan, ghostUserIDs []int) error {
	if len(ghostUserIDs) == 0 {
		return nil
	}

	ghostUserID := ghostUserIDs[rand.Intn(len(ghostUserIDs))]
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		order := &commerceschema.GroupBuyOrder{
			InitiatorId:  ghostUserID,
			PlanId:       plan.Id,
			Status:       commerceschema.GroupBuyStatusPending,
			TargetCount:  5,
			CurrentCount: 1,
			ExpiresAt:    time.Now().Add(48 * time.Hour).Unix(),
		}
		if err := tx.Create(order).Error; err != nil {
			return err
		}
		member := &commerceschema.GroupBuyMember{
			GroupBuyId:   order.Id,
			UserId:       ghostUserID,
			OrderId:      0,
			BonusGranted: true,
		}
		if err := tx.Create(member).Error; err != nil {
			return err
		}
		platformobservability.SysLog(fmt.Sprintf("created ghost group buy: plan_id=%d order_id=%d ghost_user=%d", plan.Id, order.Id, ghostUserID))
		return nil
	})
}
