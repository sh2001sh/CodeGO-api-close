package domain

import commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"

type SubscriptionSummary struct {
	Subscription *commerceschema.UserSubscription `json:"subscription"`
}

type SubscriptionPurchasePreview struct {
	Action                          string                           `json:"action"`
	BaseAmountDue                   float64                          `json:"base_amount_due"`
	AmountDue                       float64                          `json:"amount_due"`
	CurrentSubscription             *commerceschema.UserSubscription `json:"-"`
	CurrentPlan                     *commerceschema.SubscriptionPlan `json:"-"`
	DisabledReason                  string                           `json:"disabled_reason,omitempty"`
	AppliedBlindBoxDiscountRate     float64                          `json:"applied_blind_box_discount_rate,omitempty"`
	FirstPurchaseDiscountApplied    bool                             `json:"first_purchase_discount_applied,omitempty"`
	FirstPurchaseDiscountMultiplier float64                          `json:"first_purchase_discount_multiplier,omitempty"`
}

type SubscriptionPreConsumeResult struct {
	UserSubscriptionId int
	PreConsumed        int64
	AmountTotal        int64
	AmountUsedBefore   int64
	AmountUsedAfter    int64
}

type SubscriptionPlanInfo struct {
	PlanId    int
	PlanTitle string
}
