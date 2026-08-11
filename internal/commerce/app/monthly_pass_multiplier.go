package app

import (
	"errors"
	"fmt"
	"strings"

	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

const MonthlyPassGroup = "monthly-pass"

func monthlyPassDurationSeconds(plan *commerceschema.SubscriptionPlan) int64 {
	if plan == nil {
		return 0
	}
	switch commercedomain.NormalizeSubscriptionMembershipTier(plan.MembershipTier) {
	case commerceschema.SubscriptionMembershipTierLite:
		return 15 * 60
	case commerceschema.SubscriptionMembershipTierStandard:
		return 30 * 60
	case commerceschema.SubscriptionMembershipTierPro:
		return 45 * 60
	case commerceschema.SubscriptionMembershipTierUltra:
		return 60 * 60
	default:
		return 0
	}
}

func monthlyPassTitle(durationSeconds int64) string {
	return fmt.Sprintf("%d 分钟 0.1 倍率卡", durationSeconds/60)
}

func awardMonthlyPassPropTx(tx *gorm.DB, userID int, plan *commerceschema.SubscriptionPlan, reference string) error {
	if tx == nil || userID <= 0 || plan == nil || strings.TrimSpace(reference) == "" {
		return errors.New("invalid monthly pass grant")
	}
	duration := monthlyPassDurationSeconds(plan)
	if duration <= 0 || commercedomain.NormalizeSubscriptionPlanType(plan.PlanType) != commerceschema.SubscriptionPlanTypeMonthly {
		return nil
	}
	var existing commerceschema.BlindBoxProp
	err := tx.Where("prop_type = ? AND benefit_reference = ?", commerceschema.BlindBoxPropTypeMonthlyPassMultiplier, reference).First(&existing).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return tx.Create(&commerceschema.BlindBoxProp{
		UserId: userID, PropType: commerceschema.BlindBoxPropTypeMonthlyPassMultiplier,
		Title: monthlyPassTitle(duration), Status: commerceschema.BlindBoxPropStatusAvailable,
		Multiplier: 0.1, DurationSeconds: duration, RemainingSeconds: duration,
		BenefitReference: reference,
	}).Error
}

func hasActiveMonthlyPassPropTx(tx *gorm.DB, userID int) bool {
	if tx == nil || userID <= 0 {
		return false
	}
	var count int64
	err := tx.Model(&commerceschema.BlindBoxProp{}).
		Where("user_id = ? AND prop_type = ? AND status = ? AND expires_at > ?", userID, commerceschema.BlindBoxPropTypeMonthlyPassMultiplier, commerceschema.BlindBoxPropStatusActive, platformruntime.GetTimestamp()).
		Count(&count).Error
	return err == nil && count > 0
}

// IsMonthlyPassGroupActive verifies the pausable 0.1-multiplier entitlement.
func IsMonthlyPassGroupActive(userID int) bool {
	if userID <= 0 {
		return false
	}
	var active bool
	_ = platformdb.DB.Transaction(func(tx *gorm.DB) error {
		now := platformruntime.GetTimestamp()
		if err := expireUserBlindBoxPropsTx(tx, userID, now); err != nil {
			return err
		}
		active = hasActiveMonthlyPassPropTx(tx, userID)
		return nil
	})
	return active
}

// MonthlyPassConcurrentRequests returns the fixed per-user limit for the active
// card: Lite/Standard use one request, Pro/Ultra use two.
func MonthlyPassConcurrentRequests(userID int) int64 {
	if userID <= 0 {
		return 1
	}
	var prop commerceschema.BlindBoxProp
	err := platformdb.DB.Where("user_id = ? AND prop_type = ? AND status = ? AND expires_at > ?", userID, commerceschema.BlindBoxPropTypeMonthlyPassMultiplier, commerceschema.BlindBoxPropStatusActive, platformruntime.GetTimestamp()).
		Order("expires_at desc, id desc").First(&prop).Error
	if err != nil || prop.DurationSeconds < 45*60 {
		return 1
	}
	return 2
}

// BackfillActiveMonthlyPassBenefits grants one card to each currently active preset monthly subscription.
func BackfillActiveMonthlyPassBenefits() error {
	now := platformruntime.GetTimestamp()
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var subscriptions []commerceschema.UserSubscription
		if err := tx.Where("status = ? AND end_time > ?", "active", now).Find(&subscriptions).Error; err != nil {
			return err
		}
		for _, sub := range subscriptions {
			plan, err := getSubscriptionPlanRecordTx(tx, sub.PlanId)
			if err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				continue
			}
			if plan == nil || monthlyPassDurationSeconds(plan) == 0 {
				continue
			}
			reference := fmt.Sprintf("monthly-pass-backfill-20260811:%d", sub.Id)
			if err := awardMonthlyPassPropTx(tx, sub.UserId, plan, reference); err != nil {
				return err
			}
		}
		return nil
	})
}
