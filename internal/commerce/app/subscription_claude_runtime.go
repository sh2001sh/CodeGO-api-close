package app

import (
	"errors"
	"fmt"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
	"math"
	"strings"
)

// GetSubscriptionClaudeConversionConfig returns the current conversion settings snapshot.
func GetSubscriptionClaudeConversionConfig() commerceschema.SubscriptionClaudeConversionConfig {
	numerator := commerceschema.SubscriptionClaudeConversionRatioNumerator
	denominator := commerceschema.SubscriptionClaudeConversionRatioDenominator
	if numerator <= 0 {
		numerator = 1
	}
	if denominator <= 0 {
		denominator = 10
	}
	return commerceschema.SubscriptionClaudeConversionConfig{
		Enabled:          commerceschema.SubscriptionClaudeConversionEnabled,
		RatioNumerator:   numerator,
		RatioDenominator: denominator,
		ExcludeDayPass:   commerceschema.SubscriptionClaudeConversionExcludeDayPass,
	}
}

// GetUserClaudeQuota loads the user's Claude quota directly from storage.
func GetUserClaudeQuota(userID int) (int, error) {
	if userID <= 0 {
		return 0, errors.New("invalid userId")
	}
	return billingapp.GetUserClaudeWalletQuota(userID)
}

// ListRecentSubscriptionClaudeConversions returns recent conversion records for the user.
func ListRecentSubscriptionClaudeConversions(userID int, limit int) ([]commerceschema.SubscriptionClaudeConversion, error) {
	if userID <= 0 {
		return []commerceschema.SubscriptionClaudeConversion{}, nil
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	var items []commerceschema.SubscriptionClaudeConversion
	err := platformdb.DB.Where("user_id = ?", userID).Order("id desc").Limit(limit).Find(&items).Error
	return items, err
}

// GetSubscriptionPlanInfoByUserSubscriptionID resolves plan metadata for a subscription snapshot.
func GetSubscriptionPlanInfoByUserSubscriptionID(userSubscriptionID int) (*commercedomain.SubscriptionPlanInfo, error) {
	if userSubscriptionID <= 0 {
		return nil, errors.New("invalid userSubscriptionId")
	}

	sub := &commerceschema.UserSubscription{}
	if err := platformdb.DB.Where("id = ?", userSubscriptionID).First(sub).Error; err != nil {
		return nil, err
	}
	plan, err := GetSubscriptionPlanByID(sub.PlanId)
	if err != nil {
		return nil, err
	}
	return &commercedomain.SubscriptionPlanInfo{
		PlanId:    sub.PlanId,
		PlanTitle: plan.Title,
	}, nil
}

// BuildSubscriptionClaudeConversionLog formats the user-facing conversion log content.
func BuildSubscriptionClaudeConversionLog(planTitle string, sourceQuota int64, targetQuota int) string {
	return fmt.Sprintf("套餐额度转通用额度成功，套餐：%s，扣减套餐额度：%s，到账通用额度：%s", planTitle, logger.LogQuota(int(sourceQuota)), logger.LogQuota(targetQuota))
}

// BuildSubscriptionClaudeConversionPreview returns the current max convertible quota preview.
func BuildSubscriptionClaudeConversionPreview(plan *commerceschema.SubscriptionPlan, sub *commerceschema.UserSubscription) commerceschema.SubscriptionClaudeConversionPreview {
	config := GetSubscriptionClaudeConversionConfig()
	maxSourceQuota := subscriptionClaudeMaxConvertibleQuota(plan, sub, platformruntime.GetTimestamp(), config)
	return commerceschema.SubscriptionClaudeConversionPreview{
		Eligible:           maxSourceQuota > 0,
		MaxSourceQuota:     maxSourceQuota,
		PreviewClaudeQuota: calculateSubscriptionClaudeTargetQuota(maxSourceQuota, config),
	}
}

// ConvertSubscriptionQuotaToClaudeQuota converts active subscription quota into Claude quota.
func ConvertSubscriptionQuotaToClaudeQuota(requestID string, userID int, subscriptionID int, sourceQuota int64) (*commerceschema.SubscriptionClaudeConversionResult, error) {
	config := GetSubscriptionClaudeConversionConfig()
	if !config.Enabled {
		return nil, commerceschema.ErrSubscriptionClaudeConversionDisabled
	}
	if userID <= 0 || subscriptionID <= 0 {
		return nil, commerceschema.ErrSubscriptionClaudeConversionNoTarget
	}
	if strings.TrimSpace(requestID) == "" || sourceQuota <= 0 {
		return nil, commerceschema.ErrSubscriptionClaudeConversionInvalidAmount
	}

	result := &commerceschema.SubscriptionClaudeConversionResult{
		SubscriptionId: subscriptionID,
		SourceQuota:    sourceQuota,
		Config:         config,
	}
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		now := platformruntime.GetTimestamp()
		existing := &commerceschema.SubscriptionClaudeConversion{}
		if err := tx.Where("request_id = ?", requestID).First(existing).Error; err == nil {
			if existing.UserId != userID || existing.UserSubscriptionId != subscriptionID {
				return commerceschema.ErrSubscriptionClaudeConversionInvalidAmount
			}
			sub := &commerceschema.UserSubscription{}
			if err := tx.Where("id = ?", existing.UserSubscriptionId).First(sub).Error; err != nil {
				return err
			}
			claudeQuotaAfter, err := getUserClaudeQuotaTx(tx, userID)
			if err != nil {
				return err
			}
			result.Conversion = existing
			result.TargetClaudeQuota = existing.TargetClaudeQuota
			result.ClaudeQuotaAfter = claudeQuotaAfter
			result.AmountUsedAfter = sub.AmountUsed
			result.PeriodUsedAfter = sub.PeriodUsed
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		sub := &commerceschema.UserSubscription{}
		if err := tx.Set("gorm:query_option", "FOR UPDATE").
			Where("id = ? AND user_id = ?", subscriptionID, userID).
			First(sub).Error; err != nil {
			return commerceschema.ErrSubscriptionClaudeConversionNoTarget
		}
		plan, err := getSubscriptionPlanRecordTx(tx, sub.PlanId)
		if err != nil || plan == nil {
			return commerceschema.ErrSubscriptionClaudeConversionNoTarget
		}
		if sub.Status != "active" || sub.EndTime <= now {
			return commerceschema.ErrSubscriptionClaudeConversionNoTarget
		}
		if config.ExcludeDayPass && commercedomain.IsSubscriptionDayPassPlan(plan) {
			return commerceschema.ErrSubscriptionClaudeConversionNoTarget
		}
		if err := maybeResetUserSubscriptionWithPlanTx(tx, sub, plan, now); err != nil {
			return err
		}

		maxConvertible := subscriptionClaudeMaxConvertibleQuota(plan, sub, now, config)
		if maxConvertible <= 0 || sourceQuota > maxConvertible {
			return commerceschema.ErrSubscriptionClaudeConversionInsufficient
		}

		targetClaudeQuota := calculateSubscriptionClaudeTargetQuota(sourceQuota, config)
		if targetClaudeQuota <= 0 {
			return commerceschema.ErrSubscriptionClaudeConversionZeroResult
		}
		if err := applySubscriptionUsageDelta(plan, sub, "", sourceQuota); err != nil {
			return commerceschema.ErrSubscriptionClaudeConversionInsufficient
		}
		if err := tx.Save(sub).Error; err != nil {
			return err
		}
		if err := billingapp.CreditClaudeWalletQuotaTx(
			tx,
			userID,
			targetClaudeQuota,
			fmt.Sprintf("subscription-claude:%s", strings.TrimSpace(requestID)),
			"subscription_claude_conversion",
		); err != nil {
			return err
		}

		record := &commerceschema.SubscriptionClaudeConversion{
			UserId:             userID,
			UserSubscriptionId: sub.Id,
			RequestId:          strings.TrimSpace(requestID),
			Status:             commerceschema.SubscriptionClaudeConversionStatusCompleted,
			SourceQuota:        sourceQuota,
			TargetClaudeQuota:  targetClaudeQuota,
			RatioNumerator:     config.RatioNumerator,
			RatioDenominator:   config.RatioDenominator,
		}
		if err := tx.Create(record).Error; err != nil {
			return err
		}

		claudeQuotaAfter, err := getUserClaudeQuotaTx(tx, userID)
		if err != nil {
			return err
		}
		result.Conversion = record
		result.TargetClaudeQuota = targetClaudeQuota
		result.ClaudeQuotaAfter = claudeQuotaAfter
		result.AmountUsedAfter = sub.AmountUsed
		result.PeriodUsedAfter = sub.PeriodUsed
		return nil
	})
	if err != nil {
		return nil, err
	}

	_ = identitystore.InvalidateUserCache(userID)
	return result, nil
}

func calculateSubscriptionClaudeTargetQuota(sourceQuota int64, config commerceschema.SubscriptionClaudeConversionConfig) int {
	if sourceQuota <= 0 || config.RatioNumerator <= 0 || config.RatioDenominator <= 0 {
		return 0
	}
	value := float64(sourceQuota) * float64(config.RatioNumerator) / float64(config.RatioDenominator)
	return int(math.Floor(value))
}

func subscriptionClaudeMaxConvertibleQuota(plan *commerceschema.SubscriptionPlan, sub *commerceschema.UserSubscription, now int64, config commerceschema.SubscriptionClaudeConversionConfig) int64 {
	if plan == nil || sub == nil {
		return 0
	}
	if now <= 0 {
		now = platformruntime.GetTimestamp()
	}
	if sub.Status != "active" || sub.EndTime <= now {
		return 0
	}
	if config.ExcludeDayPass && commercedomain.IsSubscriptionDayPassPlan(plan) {
		return 0
	}
	if sub.AmountTotal == 0 {
		return 0
	}

	totalRemain := sub.AmountTotal - sub.AmountUsed
	if totalRemain < 0 {
		totalRemain = 0
	}
	maxConvertible := totalRemain
	periodAmount := getSubscriptionPeriodAmount(plan, sub)
	if !usesLegacySubscriptionPeriodicQuota(plan, sub) && periodAmount > 0 {
		periodRemain := periodAmount - sub.PeriodUsed
		if periodRemain < 0 {
			periodRemain = 0
		}
		if maxConvertible == 0 || periodRemain < maxConvertible {
			maxConvertible = periodRemain
		}
	}
	if maxConvertible < 0 {
		return 0
	}
	return maxConvertible
}
