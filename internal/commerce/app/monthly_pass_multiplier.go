package app

import (
	"errors"
	"fmt"
	"strings"

	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const LegacyMonthlyPassGroup = "monthly-pass"

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
	var user identityschema.User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id = ?", userID).First(&user).Error; err != nil {
		return err
	}
	now := platformruntime.GetTimestamp()
	if err := expireUserBlindBoxPropsTx(tx, userID, now); err != nil {
		return err
	}
	cards, err := lockMonthlyPassCardsForGrantTx(tx, userID)
	if err != nil {
		return err
	}
	for index := range cards {
		if hasMonthlyPassBenefitReference(cards[index].BenefitReference, reference) {
			return nil
		}
	}
	eligible := cards[:0]
	for index := range cards {
		if isEligibleMonthlyPassStatus(cards[index].Status) {
			eligible = append(eligible, cards[index])
		}
	}
	if len(eligible) > 0 {
		return mergeMonthlyPassGrantTx(tx, eligible, duration, reference, now)
	}

	return tx.Create(&commerceschema.BlindBoxProp{
		UserId: userID, PropType: commerceschema.BlindBoxPropTypeMonthlyPassMultiplier,
		Title: monthlyPassTitle(duration), Status: commerceschema.BlindBoxPropStatusAvailable,
		Multiplier: 0.1, DurationSeconds: duration, RemainingSeconds: duration,
		BenefitReference: reference,
	}).Error
}

func lockMonthlyPassCardsForGrantTx(tx *gorm.DB, userID int) ([]commerceschema.BlindBoxProp, error) {
	// Historical cards must participate in idempotency after they expire.
	var cards []commerceschema.BlindBoxProp
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ? AND prop_type = ?", userID, commerceschema.BlindBoxPropTypeMonthlyPassMultiplier).
		Order("status = 'active' desc, id asc").Find(&cards).Error
	return cards, err
}

func mergeMonthlyPassGrantTx(tx *gorm.DB, cards []commerceschema.BlindBoxProp, duration int64, reference string, now int64) error {
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
		if err := markMonthlyPassCardUsedTx(tx, cards[index].Id, now); err != nil {
			return err
		}
	}
	return nil
}

func markMonthlyPassCardUsedTx(tx *gorm.DB, propID int, now int64) error {
	return tx.Model(&commerceschema.BlindBoxProp{}).Where("id = ?", propID).Updates(map[string]any{
		"status":     commerceschema.BlindBoxPropStatusUsed,
		"used_at":    now,
		"updated_at": now,
	}).Error
}

func isEligibleMonthlyPassStatus(status string) bool {
	return status == commerceschema.BlindBoxPropStatusAvailable ||
		status == commerceschema.BlindBoxPropStatusPaused ||
		status == commerceschema.BlindBoxPropStatusActive
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

// migrateLegacyBlindBoxMultiplierProps separates old blind-box rewards from
// monthly-pass entitlements that previously shared the same prop type.
func migrateLegacyBlindBoxMultiplierProps() error {
	if !platformdb.DB.Migrator().HasTable(&commerceschema.BlindBoxProp{}) {
		return nil
	}
	return platformdb.DB.Model(&commerceschema.BlindBoxProp{}).
		Where("prop_type = ? AND open_record_id > 0", commerceschema.BlindBoxPropTypeMonthlyPassMultiplier).
		Updates(map[string]any{
			"prop_type":     commerceschema.BlindBoxPropTypeConsumeDiscount10,
			"title":         "0.1 倍率卡",
			"discount_rate": 0.90,
			"multiplier":    0.10,
			"updated_at":    platformruntime.GetTimestamp(),
		}).Error
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
