package app

import (
	"errors"

	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
	"time"
)

var (
	ErrGroupBuyNotFound       = errors.New("group buy order not found")
	ErrGroupBuyNotJoinable    = errors.New("group buy order is not joinable")
	ErrGroupBuyAlreadyJoined  = errors.New("user already joined this group buy")
	ErrGroupBuyPlanNotEnabled = errors.New("plan does not support collective benefit")
)

// BuildActiveGroupBuysPayload returns the active group-buy list payload used by the commerce API.
func BuildActiveGroupBuysPayload(userID int) (map[string]any, error) {
	items, err := listActiveGroupBuys(userID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"data":  items,
		"total": len(items),
	}, nil
}

// BuildUserGroupBuysPayload returns the current user's group-buy list payload.
func BuildUserGroupBuysPayload(userID int) (map[string]any, error) {
	items, err := listUserGroupBuys(userID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"data":  items,
		"total": len(items),
	}, nil
}

// GetGroupBuyDetail returns a single group-buy item for the current user.
func GetGroupBuyDetail(userID int, groupBuyID int64) (*commercedomain.GroupBuyListItem, error) {
	return getGroupBuyDetail(userID, groupBuyID)
}

// JoinGroupBuy joins the requested group-buy room for the current user.
func JoinGroupBuy(userID int, req GroupBuyJoinRequest) (map[string]any, error) {
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		return joinGroupBuyTx(tx, userID, req.GroupBuyId, req.OrderId, 0, 0)
	}); err != nil {
		return nil, err
	}
	return map[string]any{"joined": true}, nil
}

func listActiveGroupBuys(userID int) ([]commercedomain.GroupBuyListItem, error) {
	now := platformruntime.GetTimestamp()
	var orders []commerceschema.GroupBuyOrder
	if err := platformdb.DB.Where("status = ? AND expires_at > ? AND current_count < target_count", commerceschema.GroupBuyStatusPending, now).
		Order("created_at asc, id asc").
		Find(&orders).Error; err != nil {
		return nil, err
	}
	orders = oneGroupBuyOrderPerPlan(orders)
	items, err := hydrateGroupBuyItems(orders, userID)
	if err != nil {
		return nil, err
	}

	activePlanSet := make(map[int]struct{}, len(items))
	for _, item := range items {
		activePlanSet[item.PlanId] = struct{}{}
	}

	var plans []commerceschema.SubscriptionPlan
	if err := platformdb.DB.Where("enabled = ? AND internal_only = ? AND group_buy_enabled = ?", true, false, true).
		Order("sort_order desc, id desc").
		Find(&plans).Error; err != nil {
		return nil, err
	}
	for _, plan := range plans {
		if !supportsGroupBuyPlan(&plan) {
			continue
		}
		if _, ok := activePlanSet[plan.Id]; ok {
			continue
		}
		items = append(items, buildEmptyGroupBuyRoom(plan))
	}
	return items, nil
}

func listUserGroupBuys(userID int) ([]commercedomain.GroupBuyListItem, error) {
	if userID <= 0 {
		return []commercedomain.GroupBuyListItem{}, nil
	}

	var memberRows []commerceschema.GroupBuyMember
	if err := platformdb.DB.Where("user_id = ?", userID).Find(&memberRows).Error; err != nil {
		return nil, err
	}
	orderIDSet := make(map[int64]struct{}, len(memberRows))
	for _, row := range memberRows {
		orderIDSet[row.GroupBuyId] = struct{}{}
	}

	var initiated []commerceschema.GroupBuyOrder
	if err := platformdb.DB.Where("initiator_id = ?", userID).Find(&initiated).Error; err != nil {
		return nil, err
	}
	for _, row := range initiated {
		orderIDSet[row.Id] = struct{}{}
	}
	if len(orderIDSet) == 0 {
		return []commercedomain.GroupBuyListItem{}, nil
	}

	ids := make([]int64, 0, len(orderIDSet))
	for id := range orderIDSet {
		ids = append(ids, id)
	}
	var orders []commerceschema.GroupBuyOrder
	if err := platformdb.DB.Where("id IN ?", ids).Order("updated_at desc, id desc").Find(&orders).Error; err != nil {
		return nil, err
	}
	return hydrateGroupBuyItems(orders, userID)
}

func getGroupBuyDetail(userID int, groupBuyID int64) (*commercedomain.GroupBuyListItem, error) {
	var order commerceschema.GroupBuyOrder
	if err := platformdb.DB.Where("id = ?", groupBuyID).First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrGroupBuyNotFound
		}
		return nil, err
	}
	items, err := hydrateGroupBuyItems([]commerceschema.GroupBuyOrder{order}, userID)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ErrGroupBuyNotFound
	}
	return &items[0], nil
}

func oneGroupBuyOrderPerPlan(orders []commerceschema.GroupBuyOrder) []commerceschema.GroupBuyOrder {
	if len(orders) <= 1 {
		return orders
	}
	filtered := make([]commerceschema.GroupBuyOrder, 0, len(orders))
	seen := make(map[int]struct{}, len(orders))
	for _, order := range orders {
		if _, ok := seen[order.PlanId]; ok {
			continue
		}
		seen[order.PlanId] = struct{}{}
		filtered = append(filtered, order)
	}
	return filtered
}

func hydrateGroupBuyItems(orders []commerceschema.GroupBuyOrder, userID int) ([]commercedomain.GroupBuyListItem, error) {
	if len(orders) == 0 {
		return []commercedomain.GroupBuyListItem{}, nil
	}

	planIDs := make([]int, 0, len(orders))
	planIDSet := make(map[int]struct{}, len(orders))
	orderIDs := make([]int64, 0, len(orders))
	for _, order := range orders {
		orderIDs = append(orderIDs, order.Id)
		if _, ok := planIDSet[order.PlanId]; ok {
			continue
		}
		planIDSet[order.PlanId] = struct{}{}
		planIDs = append(planIDs, order.PlanId)
	}

	var plans []commerceschema.SubscriptionPlan
	if err := platformdb.DB.Where("id IN ?", planIDs).Find(&plans).Error; err != nil {
		return nil, err
	}
	planMap := make(map[int]commerceschema.SubscriptionPlan, len(plans))
	for _, plan := range plans {
		planMap[plan.Id] = plan
	}

	joinedSet := map[int64]struct{}{}
	if userID > 0 {
		var members []commerceschema.GroupBuyMember
		if err := platformdb.DB.Where("group_buy_id IN ? AND user_id = ?", orderIDs, userID).Find(&members).Error; err != nil {
			return nil, err
		}
		for _, member := range members {
			joinedSet[member.GroupBuyId] = struct{}{}
		}
	}

	items := make([]commercedomain.GroupBuyListItem, 0, len(orders))
	for _, order := range orders {
		plan, ok := planMap[order.PlanId]
		if !ok || !supportsGroupBuyPlan(&plan) {
			continue
		}
		_, joined := joinedSet[order.Id]
		items = append(items, buildGroupBuyItem(order, plan, joined))
	}
	return items, nil
}

func buildGroupBuyItem(order commerceschema.GroupBuyOrder, plan commerceschema.SubscriptionPlan, joined bool) commercedomain.GroupBuyListItem {
	return commercedomain.GroupBuyListItem{
		Id:           order.Id,
		PlanId:       plan.Id,
		PlanName:     plan.Title,
		PlanPrice:    plan.PriceAmount,
		Currency:     plan.Currency,
		BaseQuotaUSD: quotaUnitsToUSD(plan.TotalAmount),
		CurrentCount: order.CurrentCount,
		TargetCount:  order.TargetCount,
		BonusAt2:     plan.GroupBuyBonus2,
		BonusAt3:     plan.GroupBuyBonus3,
		BonusAt5:     plan.GroupBuyBonus5,
		ExpiresAt:    order.ExpiresAt,
		InitiatorId:  order.InitiatorId,
		Status:       order.Status,
		Joined:       joined,
	}
}

func buildEmptyGroupBuyRoom(plan commerceschema.SubscriptionPlan) commercedomain.GroupBuyListItem {
	return commercedomain.GroupBuyListItem{
		Id:           0,
		PlanId:       plan.Id,
		PlanName:     plan.Title,
		PlanPrice:    plan.PriceAmount,
		Currency:     plan.Currency,
		BaseQuotaUSD: quotaUnitsToUSD(plan.TotalAmount),
		CurrentCount: 0,
		TargetCount:  5,
		BonusAt2:     plan.GroupBuyBonus2,
		BonusAt3:     plan.GroupBuyBonus3,
		BonusAt5:     plan.GroupBuyBonus5,
		ExpiresAt:    time.Now().Add(48 * time.Hour).Unix(),
		Status:       commerceschema.GroupBuyStatusPending,
	}
}
