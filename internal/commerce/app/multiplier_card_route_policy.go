package app

import (
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

var officialMultiplierCardTypes = []string{
	commerceschema.BlindBoxPropTypeConsumeDiscount95,
	commerceschema.BlindBoxPropTypeConsumeDiscount90,
	commerceschema.BlindBoxPropTypeConsumeDiscount10,
	commerceschema.BlindBoxPropTypeZeroHourMultiplier,
	commerceschema.BlindBoxPropTypeMonthlyPassMultiplier,
}

// ActiveMultiplierCardRoutePolicy returns both routing flags from one query.
func ActiveMultiplierCardRoutePolicy(userID int) (requiresOfficial bool, monthlyPassActive bool) {
	if userID <= 0 {
		return false, false
	}
	var props []commerceschema.BlindBoxProp
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		now := platformruntime.GetTimestamp()
		if err := expireUserBlindBoxPropsTx(tx, userID, now); err != nil {
			return err
		}
		return tx.Select("prop_type").
			Where("user_id = ? AND status = ? AND prop_type IN ? AND (expires_at = 0 OR expires_at > ?)",
				userID, commerceschema.BlindBoxPropStatusActive, officialMultiplierCardTypes, now).
			Find(&props).Error
	})
	if err != nil {
		return false, false
	}
	for index := range props {
		requiresOfficial = true
		if props[index].PropType == commerceschema.BlindBoxPropTypeMonthlyPassMultiplier {
			monthlyPassActive = true
		}
	}
	return requiresOfficial, monthlyPassActive
}

// RequiresOfficialBlindBoxChannel reports whether an active multiplier card
// should constrain automatic routing to official channels.
func RequiresOfficialBlindBoxChannel(userID int) bool {
	requiresOfficial, _ := ActiveMultiplierCardRoutePolicy(userID)
	return requiresOfficial
}
