package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/sh2001sh/new-api/constant"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const luckyNumberAllocationAttempts = 5

// ensureSubscriptionLuckyNumberTx allocates the permanent public number once.
// The suffix is intentionally reusable across subscriptions; only CardCode is unique.
func ensureSubscriptionLuckyNumberTx(tx *gorm.DB, sub *commerceschema.UserSubscription, plan *commerceschema.SubscriptionPlan) (*commerceschema.SubscriptionLuckyNumber, error) {
	if tx == nil || sub == nil || plan == nil {
		return nil, errors.New("invalid lucky number arguments")
	}
	if commercedomain.NormalizeSubscriptionPlanType(plan.PlanType) != commerceschema.SubscriptionPlanTypeMonthly || !plan.LuckyDrawEnabled {
		return nil, nil
	}

	var existing commerceschema.SubscriptionLuckyNumber
	err := tx.Where("user_subscription_id = ?", sub.Id).First(&existing).Error
	if err == nil {
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	for attempt := 0; attempt < luckyNumberAllocationAttempts; attempt++ {
		suffix, suffixErr := generateLuckySuffix()
		if suffixErr != nil {
			return nil, suffixErr
		}
		cardCode, codeErr := generateLuckyCardCode(suffix)
		if codeErr != nil {
			return nil, codeErr
		}
		var collision int64
		if err := tx.Model(&commerceschema.SubscriptionLuckyNumber{}).Where("card_code = ?", cardCode).Count(&collision).Error; err != nil {
			return nil, err
		}
		if collision > 0 {
			continue
		}
		number := &commerceschema.SubscriptionLuckyNumber{
			UserSubscriptionId: sub.Id,
			UserId:             sub.UserId,
			CardCode:           cardCode,
			LuckySuffix:        suffix,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(number).Error; err != nil {
			return nil, err
		}
		if err := tx.Where("user_subscription_id = ?", sub.Id).First(&existing).Error; err == nil {
			return &existing, nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	return nil, errors.New("could not allocate a unique lucky card code")
}

func subscriptionBenefitCycleKey(sub *commerceschema.UserSubscription) string {
	if sub == nil || sub.Id <= 0 {
		return ""
	}
	if value := strings.TrimSpace(sub.LuckyBenefitCycle); value != "" {
		return value
	}
	return fmt.Sprintf("subscription-cycle:%d:%d:%d", sub.Id, sub.StartTime, sub.EndTime)
}

func setNewSubscriptionBenefitCycle(sub *commerceschema.UserSubscription) {
	if sub == nil || sub.Id <= 0 {
		return
	}
	sub.LuckyBenefitCycle = fmt.Sprintf("subscription-cycle:%d:%d:%d", sub.Id, sub.StartTime, sub.EndTime)
}

// grantSubscriptionBlindBoxBenefitsTx materializes subscription boxes as a successful,
// expiring blind-box order. The cycle row is the idempotency boundary, so retries cannot
// create another order for the same subscription cycle.
func grantSubscriptionBlindBoxBenefitsTx(tx *gorm.DB, sub *commerceschema.UserSubscription, plan *commerceschema.SubscriptionPlan, action string, previousPlan *commerceschema.SubscriptionPlan) error {
	if tx == nil || sub == nil || plan == nil {
		return errors.New("invalid subscription blind-box arguments")
	}
	if sub.Source == "admin" || sub.Source == "redemption" || sub.Source == "blind_box" {
		return nil
	}
	expected := luckyBenefitCount(plan)
	if expected <= 0 {
		return nil
	}
	cycle := subscriptionBenefitCycleKey(sub)
	if cycle == "" {
		return errors.New("subscription benefit cycle is empty")
	}

	var benefit commerceschema.SubscriptionBlindBoxBenefitCycle
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_subscription_id = ? AND benefit_cycle = ?", sub.Id, cycle).First(&benefit).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		granted := 0
		if action == commerceschema.SubscriptionPurchaseActionUpgrade && previousPlan != nil {
			granted = luckyBenefitCount(previousPlan)
			if granted > expected {
				granted = expected
			}
		}
		benefit = commerceschema.SubscriptionBlindBoxBenefitCycle{
			UserSubscriptionId: sub.Id,
			BenefitCycle:       cycle,
			UserId:             sub.UserId,
			MembershipTier:     luckyMembershipTier(plan),
			ExpectedCount:      expected,
			GrantedCount:       granted,
			Source:             commerceschema.BlindBoxOrderSourceSubscriptionBenefit,
			IdempotencyKey:     "subscription-blind-box:" + cycle,
			StartsAt:           sub.StartTime,
			EndsAt:             sub.EndTime,
			Status:             commerceschema.SubscriptionLuckyBenefitStatusPending,
		}
		if err := tx.Create(&benefit).Error; err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		if expected > benefit.ExpectedCount {
			benefit.ExpectedCount = expected
		}
		benefit.MembershipTier = luckyMembershipTier(plan)
		benefit.EndsAt = sub.EndTime
	}

	toGrant := benefit.ExpectedCount - benefit.GrantedCount
	if toGrant <= 0 {
		benefit.Status = commerceschema.SubscriptionLuckyBenefitStatusCompleted
		return tx.Save(&benefit).Error
	}
	now := platformruntime.GetTimestamp()
	order := &commerceschema.BlindBoxOrder{
		UserId:             sub.UserId,
		Quantity:           toGrant,
		OpenedCount:        0,
		Money:              0,
		TradeNo:            fmt.Sprintf("subscription-blind-box-%d-%d-%d", sub.Id, benefit.Id, benefit.GrantedCount),
		PaymentMethod:      "subscription_benefit",
		PaymentProvider:    "subscription",
		Source:             commerceschema.BlindBoxOrderSourceSubscriptionBenefit,
		UserSubscriptionId: sub.Id,
		BenefitCycle:       cycle,
		ExpiresAt:          sub.EndTime,
		Status:             constant.TopUpStatusSuccess,
		CreateTime:         now,
		CompleteTime:       now,
	}
	if err := tx.Create(order).Error; err != nil {
		return err
	}
	benefit.GrantedCount += toGrant
	benefit.Status = commerceschema.SubscriptionLuckyBenefitStatusCompleted
	return tx.Save(&benefit).Error
}

func backfillSubscriptionLuckyNumberTx(tx *gorm.DB, sub *commerceschema.UserSubscription, plan *commerceschema.SubscriptionPlan) error {
	if sub == nil || plan == nil || sub.Status != "active" || sub.EndTime <= platformruntime.GetTimestamp() {
		return nil
	}
	_, err := ensureSubscriptionLuckyNumberTx(tx, sub, plan)
	return err
}

func subscriptionLuckyNumberTableReady() bool {
	return platformdb.DB != nil && platformdb.DB.Migrator().HasTable(&commerceschema.SubscriptionLuckyNumber{})
}
