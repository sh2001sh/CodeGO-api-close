package app

import (
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
)

// GroupBuyJoinRequest captures the payload used to join an existing group buy.
type GroupBuyJoinRequest struct {
	GroupBuyId int64 `json:"group_buy_id"`
	OrderId    int   `json:"order_id"`
}

func supportsGroupBuyPlan(plan *commerceschema.SubscriptionPlan) bool {
	return plan != nil && plan.GroupBuyEnabled && !commercedomain.IsSubscriptionDayPassPlan(plan)
}
