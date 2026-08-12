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
	now := platformruntime.GetTimestamp()
	if err := expireUserBlindBoxPropsTx(tx, userID, now); err != nil {
		return err
	}
	// Keep one entitlement per user. A newly awarded card extends the
	// remaining time of an available, paused, or active card instead of
	// creating a second card that could be hidden by the active-card check.
	var cards []commerceschema.BlindBoxProp
	if err := tx.Set("gorm:query_option", "FOR UPDATE").
		Where("user_id = ? AND prop_type = ? AND status IN ?", userID, commerceschema.BlindBoxPropTypeMonthlyPassMultiplier,
			[]string{commerceschema.BlindBoxPropStatusAvailable, commerceschema.BlindBoxPropStatusPaused, commerceschema.BlindBoxPropStatusActive}).
		Order("status = 'active' desc, id asc").Find(&cards).Error; err != nil {
		return err
	}
	for index := range cards {
		if hasMonthlyPassBenefitReference(cards[index].BenefitReference, reference) {
			return nil
		}
	}
	if len(cards) > 0 {
		primary := &cards[0]
		remaining := int64(0)
		active := false
		for index := range cards {
			card := &cards[index]
			cardRemaining := card.RemainingSeconds
			if card.Status == commerceschema.BlindBoxPropStatusActive {
				cardRemaining = max(card.ExpiresAt-now, 0)
				active = true
			} else if cardRemaining <= 0 {
				cardRemaining = card.DurationSeconds
			}
			remaining += cardRemaining
		}
		remaining += duration
		primary.DurationSeconds = max(primary.DurationSeconds, duration)
		primary.RemainingSeconds = remaining
		primary.BenefitReference = appendMonthlyPassBenefitReference(primary.BenefitReference, reference)
		if active {
			primary.Status = commerceschema.BlindBoxPropStatusActive
			primary.ActivatedAt = now
			primary.ExpiresAt = now + remaining
		} else {
			primary.ExpiresAt = 0
		}
		if err := tx.Save(primary).Error; err != nil {
			return err
		}
		for index := 1; index < len(cards); index++ {
			if err := tx.Model(&commerceschema.BlindBoxProp{}).Where("id = ?", cards[index].Id).Updates(map[string]any{
				"status":     commerceschema.BlindBoxPropStatusUsed,
				"used_at":    now,
				"updated_at": now,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	}

	return tx.Create(&commerceschema.BlindBoxProp{
		UserId: userID, PropType: commerceschema.BlindBoxPropTypeMonthlyPassMultiplier,
		Title: monthlyPassTitle(duration), Status: commerceschema.BlindBoxPropStatusAvailable,
		Multiplier: 0.1, DurationSeconds: duration, RemainingSeconds: duration,
		BenefitReference: reference,
	}).Error
}

func hasMonthlyPassBenefitReference(stored, reference string) bool {
	if strings.TrimSpace(reference) == "" {
		return false
	}
	for _, item := range strings.Split(stored, "|") {
		if strings.TrimSpace(item) == reference {
			return true
		}
	}
	return false
}

func appendMonthlyPassBenefitReference(stored, reference string) string {
	if hasMonthlyPassBenefitReference(stored, reference) {
		return stored
	}
	if strings.TrimSpace(stored) == "" {
		return reference
	}
	return stored + "|" + reference
}

func primaryMonthlyPassPropTx(tx *gorm.DB, userID int) (*commerceschema.BlindBoxProp, error) {
	if tx == nil || userID <= 0 {
		return nil, errors.New("invalid monthly pass lookup")
	}
	var prop commerceschema.BlindBoxProp
	err := tx.Where("user_id = ? AND prop_type = ? AND status IN ?", userID, commerceschema.BlindBoxPropTypeMonthlyPassMultiplier,
		[]string{commerceschema.BlindBoxPropStatusAvailable, commerceschema.BlindBoxPropStatusPaused, commerceschema.BlindBoxPropStatusActive}).
		Order("status = 'active' desc, id asc").First(&prop).Error
	if err != nil {
		return nil, err
	}
	return &prop, nil
}

// ReconcileMonthlyPassProps folds legacy duplicate cards into one entitlement
// per user so route checks cannot hide an older active card behind a newer one.
func ReconcileMonthlyPassProps() (int, error) {
	now := platformruntime.GetTimestamp()
	var userIDs []int
	if err := platformdb.DB.Model(&commerceschema.BlindBoxProp{}).
		Where("prop_type = ? AND status IN ?", commerceschema.BlindBoxPropTypeMonthlyPassMultiplier,
			[]string{commerceschema.BlindBoxPropStatusAvailable, commerceschema.BlindBoxPropStatusPaused, commerceschema.BlindBoxPropStatusActive}).
		Distinct("user_id").Pluck("user_id", &userIDs).Error; err != nil {
		return 0, err
	}
	merged := 0
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		for _, userID := range userIDs {
			var cards []commerceschema.BlindBoxProp
			if err := tx.Set("gorm:query_option", "FOR UPDATE").
				Where("user_id = ? AND prop_type = ? AND status IN ?", userID, commerceschema.BlindBoxPropTypeMonthlyPassMultiplier,
					[]string{commerceschema.BlindBoxPropStatusAvailable, commerceschema.BlindBoxPropStatusPaused, commerceschema.BlindBoxPropStatusActive}).
				Order("id asc").Find(&cards).Error; err != nil {
				return err
			}
			if len(cards) <= 1 {
				continue
			}
			primaryIndex := 0
			for index := range cards {
				if cards[index].Status == commerceschema.BlindBoxPropStatusActive && cards[primaryIndex].Status != commerceschema.BlindBoxPropStatusActive {
					primaryIndex = index
				}
			}
			remaining := int64(0)
			active := false
			for index := range cards {
				cardRemaining := cards[index].RemainingSeconds
				if cards[index].Status == commerceschema.BlindBoxPropStatusActive {
					cardRemaining = max(cards[index].ExpiresAt-now, 0)
					active = true
				} else if cardRemaining <= 0 {
					cardRemaining = cards[index].DurationSeconds
				}
				remaining += cardRemaining
			}
			primary := &cards[primaryIndex]
			primary.RemainingSeconds = remaining
			if active {
				primary.Status = commerceschema.BlindBoxPropStatusActive
				primary.ActivatedAt = now
				primary.ExpiresAt = now + remaining
			} else {
				if primary.Status != commerceschema.BlindBoxPropStatusPaused {
					primary.Status = commerceschema.BlindBoxPropStatusAvailable
				}
				primary.ExpiresAt = 0
			}
			if err := tx.Save(primary).Error; err != nil {
				return err
			}
			for index := range cards {
				if index == primaryIndex {
					continue
				}
				if err := tx.Model(&commerceschema.BlindBoxProp{}).Where("id = ?", cards[index].Id).Updates(map[string]any{
					"status": commerceschema.BlindBoxPropStatusUsed, "used_at": now, "updated_at": now,
				}).Error; err != nil {
					return err
				}
				merged++
			}
		}
		return nil
	})
	return merged, err
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
			if subscriptionLuckyNumberTableReady() {
				if err := backfillSubscriptionLuckyNumberTx(tx, &sub, plan); err != nil {
					return err
				}
			}
			if monthlyPassDurationSeconds(plan) == 0 {
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
