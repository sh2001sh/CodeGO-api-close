package app

import commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"

// RedemptionResult describes the outcome of one user redemption operation.
type RedemptionResult struct {
	RedeemType         string                              `json:"redeem_type"`
	Quota              int                                 `json:"quota,omitempty"`
	WalletType         string                              `json:"wallet_type,omitempty"`
	PlanId             int                                 `json:"plan_id,omitempty"`
	PlanTitle          string                              `json:"plan_title,omitempty"`
	BlindBoxQuantity   int                                 `json:"blind_box_quantity,omitempty"`
	BlindBoxOrderId    int                                 `json:"blind_box_order_id,omitempty"`
	BlindBoxOpenCount  int                                 `json:"blind_box_open_count,omitempty"`
	BlindBoxRecords    []commerceschema.BlindBoxOpenRecord `json:"blind_box_records,omitempty"`
	UserSubscriptionId int                                 `json:"user_subscription_id,omitempty"`
}
