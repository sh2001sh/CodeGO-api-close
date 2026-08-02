package app

import (
	"errors"

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
