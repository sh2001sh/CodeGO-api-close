package app

import (
	"errors"
	"strings"

	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// ApplyBlindBoxConsumptionDiscount atomically applies and audits one request's
// official-channel multiplier-card benefit.
func ApplyBlindBoxConsumptionDiscount(request billingapp.BlindBoxConsumptionDiscountRequest) (billingapp.BlindBoxConsumptionDiscountResult, error) {
	result := undiscountedBlindBoxResult(request.Quota)
	if request.UserID <= 0 || request.Quota <= 0 || strings.TrimSpace(request.RequestID) == "" {
		return result, nil
	}
	if strings.ToLower(strings.TrimSpace(request.ChannelScope)) != gatewayschema.ChannelScopeOfficial {
		return result, nil
	}

	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if existing, err := loadBlindBoxDiscountUsageTx(tx, request.RequestID); err == nil {
			result = blindBoxDiscountResultFromUsage(existing)
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		now := platformruntime.GetTimestamp()
		if err := expireUserBlindBoxPropsTx(tx, request.UserID, now); err != nil {
			return err
		}
		prop, err := selectBlindBoxConsumptionPropTx(tx, request, now)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}

		quotaBefore, quotaAfter := calculatePropDiscountQuota(*prop, request.Quota)
		discountQuota := int64(quotaBefore - quotaAfter)
		if quotaBefore <= 0 || discountQuota <= 0 {
			return nil
		}
		usedAfter := prop.UsedDiscountQuota + discountQuota

		update := tx.Model(&commerceschema.BlindBoxProp{}).
			Where("id = ? AND used_discount_quota = ? AND status = ?", prop.Id, prop.UsedDiscountQuota, prop.Status).
			Updates(map[string]any{
				"used_discount_quota": usedAfter,
				"updated_at":          now,
			})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return errors.New("blind box prop discount was updated concurrently")
		}

		nominalMultiplier := normalizeDiscountMultiplier(prop.Multiplier)
		effectiveMultiplier := normalizeDiscountMultiplier(float64(quotaAfter) / float64(quotaBefore))
		usage := &commerceschema.BlindBoxPropDiscountUsage{
			RequestId: request.RequestID, UserId: request.UserID, PropId: prop.Id, PropTitle: prop.Title,
			ChannelId: request.ChannelID, ChannelScope: gatewayschema.ChannelScopeOfficial,
			ModelName: request.ModelName, QuotaBeforeDiscount: int64(quotaBefore),
			QuotaAfterDiscount: int64(quotaAfter), DiscountQuota: discountQuota,
			DiscountRate:        normalizeDiscountMultiplier(float64(discountQuota) / float64(quotaBefore)),
			Multiplier:          nominalMultiplier,
			EffectiveMultiplier: effectiveMultiplier,
			RemainingQuota:      0, CreatedAt: now,
		}
		if err := tx.Create(usage).Error; err != nil {
			return err
		}
		result = blindBoxDiscountResultFromUsage(usage)
		return nil
	})
	if err == nil {
		return result, nil
	}

	var existing commerceschema.BlindBoxPropDiscountUsage
	if lookupErr := platformdb.DB.Where("request_id = ?", request.RequestID).First(&existing).Error; lookupErr == nil {
		return blindBoxDiscountResultFromUsage(&existing), nil
	}
	return undiscountedBlindBoxResult(request.Quota), err
}

func selectBlindBoxConsumptionPropTx(tx *gorm.DB, request billingapp.BlindBoxConsumptionDiscountRequest, now int64) (*commerceschema.BlindBoxProp, error) {
	var props []commerceschema.BlindBoxProp
	err := tx.Set("gorm:query_option", "FOR UPDATE").
		Where("user_id = ? AND status = ? AND prop_type IN ? AND expires_at > ?", request.UserID, commerceschema.BlindBoxPropStatusActive, []string{
			commerceschema.BlindBoxPropTypeConsumeDiscount95,
			commerceschema.BlindBoxPropTypeConsumeDiscount90,
			commerceschema.BlindBoxPropTypeConsumeDiscount10,
		}, now).
		Order("multiplier asc, id asc").Find(&props).Error
	if err != nil {
		return nil, err
	}
	for index := range props {
		return &props[index], nil
	}
	return nil, gorm.ErrRecordNotFound
}

func calculatePropDiscountQuota(prop commerceschema.BlindBoxProp, chargedQuota int) (int, int) {
	if chargedQuota <= 0 || prop.Multiplier <= 0 || prop.Multiplier >= 1 {
		return chargedQuota, chargedQuota
	}
	quotaAfter := int(decimal.NewFromFloat(prop.Multiplier).
		Mul(decimal.NewFromInt(int64(chargedQuota))).Round(0).IntPart())
	return chargedQuota, quotaAfter
}

func normalizeDiscountMultiplier(value float64) float64 {
	return decimal.NewFromFloat(value).Round(4).InexactFloat64()
}

func loadBlindBoxDiscountUsageTx(tx *gorm.DB, requestID string) (*commerceschema.BlindBoxPropDiscountUsage, error) {
	var usage commerceschema.BlindBoxPropDiscountUsage
	if err := tx.Where("request_id = ?", requestID).First(&usage).Error; err != nil {
		return nil, err
	}
	return &usage, nil
}

func blindBoxDiscountResultFromUsage(usage *commerceschema.BlindBoxPropDiscountUsage) billingapp.BlindBoxConsumptionDiscountResult {
	if usage == nil {
		return billingapp.BlindBoxConsumptionDiscountResult{Multiplier: 1, NominalMultiplier: 1, EffectiveMultiplier: 1}
	}
	return billingapp.BlindBoxConsumptionDiscountResult{
		PropID: usage.PropId, Title: usage.PropTitle, QuotaBeforeDiscount: int(usage.QuotaBeforeDiscount),
		QuotaAfterDiscount: int(usage.QuotaAfterDiscount), DiscountQuota: int(usage.DiscountQuota),
		DiscountRate: usage.DiscountRate, Multiplier: usage.Multiplier,
		NominalMultiplier: usage.Multiplier, EffectiveMultiplier: effectiveMultiplierFromUsage(usage),
		RemainingDiscountQuota: usage.RemainingQuota,
	}
}

func effectiveMultiplierFromUsage(usage *commerceschema.BlindBoxPropDiscountUsage) float64 {
	if usage == nil {
		return 1
	}
	if usage.EffectiveMultiplier > 0 {
		return normalizeDiscountMultiplier(usage.EffectiveMultiplier)
	}
	// Compatibility for rows created before effective_multiplier existed.
	return normalizeDiscountMultiplier(usage.Multiplier)
}

func undiscountedBlindBoxResult(quota int) billingapp.BlindBoxConsumptionDiscountResult {
	return billingapp.BlindBoxConsumptionDiscountResult{
		QuotaBeforeDiscount: quota,
		QuotaAfterDiscount:  quota,
		Multiplier:          1,
		NominalMultiplier:   1,
		EffectiveMultiplier: 1,
	}
}
