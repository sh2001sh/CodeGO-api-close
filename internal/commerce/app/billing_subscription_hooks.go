package app

import (
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
)

func init() {
	billingapp.RegisterSubscriptionFundingHooks(billingapp.SubscriptionFundingHooks{
		PreConsume: func(requestID string, userID int, modelName string, amount int64) (*billingapp.SubscriptionFundingPreConsumeResult, error) {
			result, err := PreConsumeUserSubscription(requestID, userID, modelName, amount)
			if err != nil {
				return nil, err
			}
			if result == nil {
				return nil, nil
			}
			return &billingapp.SubscriptionFundingPreConsumeResult{
				UserSubscriptionID:        result.UserSubscriptionId,
				PreConsumed:               result.PreConsumed,
				AmountTotal:               result.AmountTotal,
				AmountUsedAfter:           result.AmountUsedAfter,
				ReservationID:             result.ReservationID,
				AccountID:                 result.AccountID,
				PlanID:                    result.PlanID,
				PlanTitle:                 result.PlanTitle,
				CreditPackGrantedQuota:    result.CreditPackGrantedQuota,
				CreditPackPaidQuota:       result.CreditPackPaidQuota,
				CreditPackChannelShareBPS: result.CreditPackChannelShareBPS,
			}, nil
		},
		PreConsumeCreditPack: func(requestID string, userID int, modelName string, amount int64) (*billingapp.SubscriptionFundingPreConsumeResult, error) {
			result, err := PreConsumeCreditPack(requestID, userID, modelName, amount)
			if err != nil || result == nil {
				return nil, err
			}
			return creditPackFundingResult(result), nil
		},
		ReserveAdditional: func(requestID string, subscriptionID int, modelName string, amount int64) error {
			return ReserveAdditionalSubscriptionQuota(requestID, subscriptionID, modelName, amount)
		},
		ReserveUnlimitedTarget: ReserveUnlimitedSubscriptionQuotaTarget,
		GetPlanInfo: func(userSubscriptionID int) (*billingapp.SubscriptionFundingPlanInfo, error) {
			planInfo, err := GetSubscriptionPlanInfoByUserSubscriptionID(userSubscriptionID)
			if err != nil {
				return nil, err
			}
			if planInfo == nil {
				return nil, nil
			}
			return &billingapp.SubscriptionFundingPlanInfo{
				PlanID:    planInfo.PlanId,
				PlanTitle: planInfo.PlanTitle,
			}, nil
		},
		PostConsumeDelta: func(subscriptionID int, modelName string, delta int64) error {
			return PostConsumeUserSubscriptionUsageDelta(subscriptionID, modelName, delta)
		},
		AdjustUsageDelta: AdjustUserSubscriptionUsageDelta,
		RefundPreConsume: func(requestID string) error {
			return RefundSubscriptionPreConsume(requestID)
		},
		SettleReservation: func(requestID string, subscriptionID int, modelName string, actualAmount int64) error {
			return SettleSubscriptionReservation(requestID, subscriptionID, modelName, actualAmount)
		},
		SettleReservationTx: SettleSubscriptionReservationTx,
		GetMonthlyPassEntitlement: func(userID int) (*billingapp.MonthlyPassEntitlement, error) {
			entitlement, err := ActiveMonthlyPassEntitlement(userID)
			if err != nil || entitlement == nil {
				return nil, err
			}
			return &billingapp.MonthlyPassEntitlement{
				PropID: entitlement.PropID, Multiplier: entitlement.Multiplier, ExpiresAt: entitlement.ExpiresAt,
			}, nil
		},
		ValidateMonthlyPassEntitlement: func(userID int, entitlement billingapp.MonthlyPassEntitlement) (bool, error) {
			return ValidateMonthlyPassEntitlement(userID, MonthlyPassEntitlement{
				PropID: entitlement.PropID, Multiplier: entitlement.Multiplier, ExpiresAt: entitlement.ExpiresAt,
			})
		},
		ApplyBlindBoxConsumptionDiscount: ApplyBlindBoxConsumptionDiscount,
	})
}

func creditPackFundingResult(result *commercedomain.SubscriptionPreConsumeResult) *billingapp.SubscriptionFundingPreConsumeResult {
	return &billingapp.SubscriptionFundingPreConsumeResult{
		UserSubscriptionID: result.UserSubscriptionId, PreConsumed: result.PreConsumed,
		AmountTotal: result.AmountTotal, AmountUsedAfter: result.AmountUsedAfter,
		ReservationID: result.ReservationID, AccountID: result.AccountID,
		PlanID: result.PlanID, PlanTitle: result.PlanTitle,
		CreditPackGrantedQuota:    result.CreditPackGrantedQuota,
		CreditPackPaidQuota:       result.CreditPackPaidQuota,
		CreditPackChannelShareBPS: result.CreditPackChannelShareBPS,
	}
}
