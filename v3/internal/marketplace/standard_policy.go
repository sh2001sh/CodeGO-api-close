package marketplace

import "github.com/sh2001sh/new-api/v3/pkg/credits"

// StandardPolicy retains the standard pool's conditional branches, which run
// before its ordinary tiers and differ from credits first/small/big policies.
// Probabilities use parts per billion; every monetary field uses micro-credits.
type StandardPolicy struct {
	Enabled                    bool          `json:"enabled"`
	SubscriptionProbabilityPPB int64         `json:"subscription_probability_ppb"`
	SubscriptionPlanID         int64         `json:"subscription_plan_id"`
	FirstPurchaseMinimumMicro  credits.Micro `json:"first_purchase_minimum_micro"`
	PityMinimumMicro           credits.Micro `json:"pity_minimum_micro"`
	LowRewardThresholdMicro    credits.Micro `json:"low_reward_threshold_micro"`
	PityAfter                  int           `json:"pity_after"`
}

func validateStandard(p StandardPolicy) error {
	if p.SubscriptionProbabilityPPB < 0 || p.SubscriptionProbabilityPPB > 1000000000 || p.SubscriptionPlanID < 0 || p.FirstPurchaseMinimumMicro < 0 || p.PityMinimumMicro < 0 || p.LowRewardThresholdMicro < 0 || p.PityAfter < 0 || p.PityAfter > 1000000 {
		return ErrInvalidInput
	}
	if p.Enabled && p.SubscriptionProbabilityPPB > 0 && p.SubscriptionPlanID == 0 {
		return ErrInvalidInput
	}
	return nil
}
