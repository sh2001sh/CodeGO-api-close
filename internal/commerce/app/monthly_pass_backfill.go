package app

import (
	"errors"
	"fmt"

	"github.com/sh2001sh/new-api/constant"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

// BackfillActiveMonthlyPassBenefits grants one migration benefit to eligible
// subscriptions that do not already have an order-funded multiplier card.
func BackfillActiveMonthlyPassBenefits() error {
	now := platformruntime.GetTimestamp()
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var subscriptions []commerceschema.UserSubscription
		if err := tx.Where("status = ? AND end_time > ?", "active", now).Find(&subscriptions).Error; err != nil {
			return err
		}
		for index := range subscriptions {
			if err := backfillMonthlyPassSubscriptionTx(tx, &subscriptions[index]); err != nil {
				return err
			}
		}
		return nil
	})
}

func backfillMonthlyPassSubscriptionTx(tx *gorm.DB, sub *commerceschema.UserSubscription) error {
	plan, err := getSubscriptionPlanRecordTx(tx, sub.PlanId)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if subscriptionLuckyNumberTableReady() {
		if err := backfillSubscriptionLuckyNumberTx(tx, sub, plan); err != nil {
			return err
		}
	}
	if monthlyPassDurationSeconds(plan) == 0 {
		return nil
	}
	hasOrderBenefit, err := subscriptionHasMonthlyPassOrderBenefitTx(tx, sub)
	if err != nil || hasOrderBenefit {
		return err
	}
	reference := fmt.Sprintf("monthly-pass-backfill-20260811:%d", sub.Id)
	return awardMonthlyPassPropTx(tx, sub.UserId, plan, reference)
}

func subscriptionHasMonthlyPassOrderBenefitTx(tx *gorm.DB, sub *commerceschema.UserSubscription) (bool, error) {
	var orderIDs []int
	if err := tx.Model(&commerceschema.SubscriptionOrder{}).
		Where("user_id = ? AND target_subscription_id = ? AND status = ? AND fulfillment_status = ?",
			sub.UserId, sub.Id, constant.TopUpStatusSuccess, commerceschema.SubscriptionOrderFulfillmentCompleted).
		Pluck("id", &orderIDs).Error; err != nil || len(orderIDs) == 0 {
		return false, err
	}
	var cards []commerceschema.BlindBoxProp
	if err := tx.Select("benefit_reference").
		Where("user_id = ? AND prop_type = ?", sub.UserId, commerceschema.BlindBoxPropTypeMonthlyPassMultiplier).
		Find(&cards).Error; err != nil {
		return false, err
	}
	for _, orderID := range orderIDs {
		reference := fmt.Sprintf("monthly-pass-order:%d", orderID)
		for index := range cards {
			if hasMonthlyPassBenefitReference(cards[index].BenefitReference, reference) {
				return true, nil
			}
		}
	}
	return false, nil
}
