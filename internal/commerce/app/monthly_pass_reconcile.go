package app

import (
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ReconcileMonthlyPassProps folds current duplicate cards into one entitlement.
func ReconcileMonthlyPassProps() (int, error) {
	var userIDs []int
	if err := platformdb.DB.Model(&commerceschema.BlindBoxProp{}).
		Where("prop_type = ? AND status IN ?", commerceschema.BlindBoxPropTypeMonthlyPassMultiplier, currentMonthlyPassStatuses()).
		Distinct("user_id").Pluck("user_id", &userIDs).Error; err != nil {
		return 0, err
	}
	merged := 0
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		for _, userID := range userIDs {
			count, err := reconcileUserMonthlyPassPropsTx(tx, userID, platformruntime.GetTimestamp())
			if err != nil {
				return err
			}
			merged += count
		}
		return nil
	})
	return merged, err
}

func reconcileUserMonthlyPassPropsTx(tx *gorm.DB, userID int, now int64) (int, error) {
	var cards []commerceschema.BlindBoxProp
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ? AND prop_type = ? AND status IN ?", userID,
			commerceschema.BlindBoxPropTypeMonthlyPassMultiplier, currentMonthlyPassStatuses()).
		Order("id asc").Find(&cards).Error; err != nil || len(cards) <= 1 {
		return 0, err
	}
	primaryIndex, remaining, active := summarizeMonthlyPassCards(cards, now)
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
		return 0, err
	}
	merged := 0
	for index := range cards {
		if index == primaryIndex {
			continue
		}
		if err := markMonthlyPassCardUsedTx(tx, cards[index].Id, now); err != nil {
			return 0, err
		}
		merged++
	}
	return merged, nil
}

func summarizeMonthlyPassCards(cards []commerceschema.BlindBoxProp, now int64) (primaryIndex int, remaining int64, active bool) {
	for index := range cards {
		if cards[index].Status == commerceschema.BlindBoxPropStatusActive && cards[primaryIndex].Status != commerceschema.BlindBoxPropStatusActive {
			primaryIndex = index
		}
		cardRemaining := cards[index].RemainingSeconds
		if cards[index].Status == commerceschema.BlindBoxPropStatusActive {
			cardRemaining = max(cards[index].ExpiresAt-now, 0)
			active = true
		} else if cardRemaining <= 0 {
			cardRemaining = cards[index].DurationSeconds
		}
		remaining += cardRemaining
	}
	return primaryIndex, remaining, active
}

func currentMonthlyPassStatuses() []string {
	return []string{
		commerceschema.BlindBoxPropStatusAvailable,
		commerceschema.BlindBoxPropStatusPaused,
		commerceschema.BlindBoxPropStatusActive,
	}
}
