package app

import (
	"errors"

	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

// MonthlyPassEntitlement is the immutable card window attached to one request.
type MonthlyPassEntitlement struct {
	PropID     int
	Multiplier float64
	ExpiresAt  int64
}

// ActiveMonthlyPassEntitlement returns the exact active package card.
func ActiveMonthlyPassEntitlement(userID int) (*MonthlyPassEntitlement, error) {
	if userID <= 0 {
		return nil, nil
	}
	var entitlement *MonthlyPassEntitlement
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		now := platformruntime.GetTimestamp()
		if err := expireUserBlindBoxPropsTx(tx, userID, now); err != nil {
			return err
		}
		var prop commerceschema.BlindBoxProp
		err := tx.Where("user_id = ? AND prop_type = ? AND status = ? AND expires_at > ?",
			userID, commerceschema.BlindBoxPropTypeMonthlyPassMultiplier, commerceschema.BlindBoxPropStatusActive, now).
			Order("id asc").First(&prop).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		entitlement = &MonthlyPassEntitlement{PropID: prop.Id, Multiplier: prop.Multiplier, ExpiresAt: prop.ExpiresAt}
		return nil
	})
	return entitlement, err
}

// ValidateMonthlyPassEntitlement verifies the exact card and original expiry.
func ValidateMonthlyPassEntitlement(userID int, entitlement MonthlyPassEntitlement) (bool, error) {
	if userID <= 0 || entitlement.PropID <= 0 || entitlement.ExpiresAt <= 0 || entitlement.Multiplier <= 0 || entitlement.Multiplier >= 1 {
		return false, nil
	}
	if platformruntime.GetTimestamp() >= entitlement.ExpiresAt {
		return false, nil
	}
	var count int64
	err := platformdb.DB.Model(&commerceschema.BlindBoxProp{}).
		Where("id = ? AND user_id = ? AND prop_type = ? AND status = ? AND multiplier = ? AND expires_at >= ?",
			entitlement.PropID, userID, commerceschema.BlindBoxPropTypeMonthlyPassMultiplier,
			commerceschema.BlindBoxPropStatusActive, entitlement.Multiplier, entitlement.ExpiresAt).
		Count(&count).Error
	return count > 0, err
}

// ActiveMonthlyPassMultiplier returns the active package-only multiplier.
func ActiveMonthlyPassMultiplier(userID int) float64 {
	entitlement, err := ActiveMonthlyPassEntitlement(userID)
	if err == nil && entitlement != nil {
		return entitlement.Multiplier
	}
	return 1
}

// IsMonthlyPassMultiplierActive reports whether the package benefit is active.
func IsMonthlyPassMultiplierActive(userID int) bool {
	return ActiveMonthlyPassMultiplier(userID) < 1
}
