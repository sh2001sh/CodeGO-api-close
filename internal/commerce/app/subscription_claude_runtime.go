package app

import (
	"errors"
	"fmt"
	"strings"

	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GetSubscriptionClaudeConversionConfig returns the current conversion switch.
func GetSubscriptionClaudeConversionConfig() commerceschema.SubscriptionClaudeConversionConfig {
	return commerceschema.SubscriptionClaudeConversionConfig{
		Enabled: commerceschema.SubscriptionClaudeConversionEnabled,
	}
}

// GetUserUnifiedCredit loads the unified credit balance.
func GetUserUnifiedCredit(userID int) (int, error) {
	if userID <= 0 {
		return 0, errors.New("invalid userId")
	}
	return billingapp.GetUserClaudeWalletQuota(userID)
}

// ListRecentSubscriptionClaudeConversions returns recent monthly-pass settlements.
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
	return &commercedomain.SubscriptionPlanInfo{PlanId: sub.PlanId, PlanTitle: plan.Title}, nil
}

// BuildSubscriptionClaudeConversionLog formats the user-facing settlement log.
func BuildSubscriptionClaudeConversionLog(planTitle string, unusedRatio float64, targetQuota int) string {
	return fmt.Sprintf("月卡转通用额度成功，月卡：%s，未使用比例：%.2f%%，到账通用额度：%s", planTitle, unusedRatio*100, logger.LogQuota(targetQuota))
}

// BuildSubscriptionClaudeConversionPreview calculates the cash-out value of the whole monthly pass.
func BuildSubscriptionClaudeConversionPreview(plan *commerceschema.SubscriptionPlan, sub *commerceschema.UserSubscription) commerceschema.SubscriptionClaudeConversionPreview {
	quote := calculateMonthlyPassConversionQuote(plan, sub, platformruntime.GetTimestamp())
	return commerceschema.SubscriptionClaudeConversionPreview{
		Eligible:        quote.targetQuota > 0,
		RemainingQuota:  quote.remainingQuota,
		PlanPriceAmount: quote.planPriceAmount,
		UnusedRatio:     quote.unusedRatio,
		PreviewQuota:    quote.targetQuota,
	}
}

type monthlyPassConversionQuote struct {
	remainingQuota  int64
	planPriceAmount float64
	unusedRatio     float64
	targetQuota     int
}

// ConvertMonthlyPassToUnifiedCredit settles one whole monthly pass by its unused percentage.
func ConvertMonthlyPassToUnifiedCredit(requestID string, userID int, subscriptionID int) (*commerceschema.SubscriptionClaudeConversionResult, error) {
	config := GetSubscriptionClaudeConversionConfig()
	if !config.Enabled {
		return nil, commerceschema.ErrSubscriptionClaudeConversionDisabled
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" || userID <= 0 || subscriptionID <= 0 {
		return nil, commerceschema.ErrSubscriptionClaudeConversionInvalid
	}

	result := &commerceschema.SubscriptionClaudeConversionResult{
		SubscriptionId: subscriptionID,
		Config:         config,
	}
	downgradeGroup := ""
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		existing := &commerceschema.SubscriptionClaudeConversion{}
		if err := tx.Where("request_id = ?", requestID).First(existing).Error; err == nil {
			if existing.UserId != userID || existing.UserSubscriptionId != subscriptionID {
				return commerceschema.ErrSubscriptionClaudeConversionInvalid
			}
			return loadExistingMonthlyPassConversionResult(tx, result, existing)
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		sub := &commerceschema.UserSubscription{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND user_id = ?", subscriptionID, userID).
			First(sub).Error; err != nil {
			return commerceschema.ErrSubscriptionClaudeConversionNoTarget
		}
		plan, err := getSubscriptionPlanRecordTx(tx, sub.PlanId)
		if err != nil || plan == nil {
			return commerceschema.ErrSubscriptionClaudeConversionNoTarget
		}
		now := platformruntime.GetTimestamp()
		if err := maybeResetUserSubscriptionWithPlanTx(tx, sub, plan, now); err != nil {
			return err
		}
		quote := calculateMonthlyPassConversionQuote(plan, sub, now)
		if quote.targetQuota <= 0 {
			return commerceschema.ErrSubscriptionClaudeConversionNoTarget
		}
		if pending, err := monthlyPassHasPendingReservationTx(tx, sub.Id); err != nil {
			return err
		} else if pending {
			return commerceschema.ErrSubscriptionClaudeConversionInProgress
		}

		sub.AmountUsed = sub.AmountTotal
		if periodAmount := getSubscriptionPeriodAmount(plan, sub); periodAmount > 0 {
			sub.PeriodUsed = periodAmount
		}
		sub.Status = "cancelled"
		sub.EndTime = now
		if err := tx.Save(sub).Error; err != nil {
			return err
		}
		downgradeGroup, err = downgradeUserGroupForSubscriptionTx(tx, sub, now)
		if err != nil {
			return err
		}

		if err := billingapp.CreditClaudeWalletQuotaTx(
			tx,
			userID,
			quote.targetQuota,
			fmt.Sprintf("monthly-pass-conversion:%s", requestID),
			"monthly_pass_conversion",
		); err != nil {
			return err
		}

		record := &commerceschema.SubscriptionClaudeConversion{
			UserId:             userID,
			UserSubscriptionId: sub.Id,
			RequestId:          requestID,
			Status:             commerceschema.SubscriptionClaudeConversionStatusCompleted,
			SourceQuota:        quote.remainingQuota,
			TargetQuota:        quote.targetQuota,
			PlanPriceAmount:    quote.planPriceAmount,
			UnusedRatio:        quote.unusedRatio,
			RatioNumerator:     0,
			RatioDenominator:   0,
		}
		if err := tx.Create(record).Error; err != nil {
			return err
		}

		result.Conversion = record
		result.SourceQuota = quote.remainingQuota
		result.TargetQuota = quote.targetQuota
		result.PlanPriceAmount = quote.planPriceAmount
		result.UnusedRatio = quote.unusedRatio
		result.AmountUsedAfter = sub.AmountUsed
		result.PeriodUsedAfter = sub.PeriodUsed
		result.QuotaAfter, err = getUserClaudeQuotaTx(tx, userID)
		return err
	})
	if err != nil {
		return nil, err
	}

	_ = identitystore.InvalidateUserCache(userID)
	if downgradeGroup != "" {
		_ = identitystore.UpdateUserGroupCache(userID, downgradeGroup)
	}
	return result, nil
}

func loadExistingMonthlyPassConversionResult(tx *gorm.DB, result *commerceschema.SubscriptionClaudeConversionResult, existing *commerceschema.SubscriptionClaudeConversion) error {
	sub := &commerceschema.UserSubscription{}
	if err := tx.Where("id = ?", existing.UserSubscriptionId).First(sub).Error; err != nil {
		return err
	}
	balance, err := getUserClaudeQuotaTx(tx, existing.UserId)
	if err != nil {
		return err
	}
	result.Conversion = existing
	result.SourceQuota = existing.SourceQuota
	result.TargetQuota = existing.TargetQuota
	result.QuotaAfter = balance
	result.AmountUsedAfter = sub.AmountUsed
	result.PeriodUsedAfter = sub.PeriodUsed
	result.PlanPriceAmount = existing.PlanPriceAmount
	result.UnusedRatio = existing.UnusedRatio
	return nil
}

func calculateMonthlyPassConversionQuote(plan *commerceschema.SubscriptionPlan, sub *commerceschema.UserSubscription, now int64) monthlyPassConversionQuote {
	if plan == nil || sub == nil || sub.Status != "active" || sub.EndTime <= now || plan.PlanType != commerceschema.SubscriptionPlanTypeMonthly || plan.PriceAmount <= 0 || sub.AmountTotal <= 0 {
		return monthlyPassConversionQuote{}
	}
	remaining := sub.AmountTotal - sub.AmountUsed
	if remaining <= 0 {
		return monthlyPassConversionQuote{}
	}
	unusedRatio := decimal.NewFromInt(remaining).Div(decimal.NewFromInt(sub.AmountTotal))
	target := decimal.NewFromFloat(plan.PriceAmount).
		Mul(unusedRatio).
		Mul(decimal.NewFromFloat(platformruntime.QuotaPerUnit)).
		Floor().
		IntPart()
	if target <= 0 {
		return monthlyPassConversionQuote{}
	}
	return monthlyPassConversionQuote{
		remainingQuota:  remaining,
		planPriceAmount: plan.PriceAmount,
		unusedRatio:     unusedRatio.InexactFloat64(),
		targetQuota:     int(target),
	}
}

func monthlyPassHasPendingReservationTx(tx *gorm.DB, subscriptionID int) (bool, error) {
	var account billingschema.BillingAccount
	err := tx.Where("account_type = ? AND owner_type = ? AND owner_id = ?", "subscription", "user_subscription", subscriptionID).First(&account).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var snapshot billingschema.BillingBalanceSnapshot
	err = tx.Where("account_id = ?", account.AccountID).First(&snapshot).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return snapshot.ReservedBalance > 0, err
}
