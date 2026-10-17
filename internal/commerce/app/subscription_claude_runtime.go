package app

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
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
		Enabled:      commerceschema.SubscriptionClaudeConversionEnabled,
		BonusPercent: 0,
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
	if err := platformdb.DB.Where("user_id = ?", userID).Order("id desc").Limit(limit).Find(&items).Error; err != nil {
		return nil, err
	}
	if err := attachMonthlyPassConversionReceipts(platformdb.DB, items); err != nil {
		return nil, err
	}
	return items, nil
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
func BuildSubscriptionClaudeConversionLog(planTitle string, conversionPercent int, targetQuota int) string {
	return fmt.Sprintf("旧套餐转余额成功，套餐：%s，转换比例：%d%%，到账余额：%s", planTitle, conversionPercent, logger.LogQuota(targetQuota))
}

// BuildSubscriptionClaudeConversionPreview calculates the cash-out value of the whole monthly pass.
func BuildSubscriptionClaudeConversionPreview(plan *commerceschema.SubscriptionPlan, sub *commerceschema.UserSubscription) commerceschema.SubscriptionClaudeConversionPreview {
	if plan == nil || sub == nil {
		return commerceschema.SubscriptionClaudeConversionPreview{}
	}
	pricedPlan, err := legacyConversionPaidPlanTx(platformdb.DB, plan, sub)
	if err != nil {
		return commerceschema.SubscriptionClaudeConversionPreview{IneligibleReason: err.Error()}
	}
	plan = pricedPlan
	resetUsed, err := subscriptionHasResetOpportunityUsageTx(platformdb.DB, sub.Id)
	if err != nil {
		return commerceschema.SubscriptionClaudeConversionPreview{IneligibleReason: err.Error()}
	}
	if resetUsed {
		return commerceschema.SubscriptionClaudeConversionPreview{IneligibleReason: commerceschema.ErrSubscriptionClaudeConversionResetUsed.Error()}
	}
	quote := calculateMonthlyPassConversionQuote(plan, sub, platformruntime.GetTimestamp(), 0)
	group, err := monthlyPassConversionGroupTx(platformdb.DB, sub)
	if err != nil {
		return commerceschema.SubscriptionClaudeConversionPreview{IneligibleReason: err.Error()}
	}
	applyMonthlyPassGroupBonus(&quote, group)
	return commerceschema.SubscriptionClaudeConversionPreview{
		Eligible:             quote.targetQuota > 0,
		RemainingQuota:       quote.remainingQuota,
		PlanPriceAmount:      quote.planPriceAmount,
		UnusedRatio:          quote.unusedRatio,
		MaxConversionPercent: quote.maxConversionPercent,
		PreviewQuota:         quote.targetQuota,
		BaseQuota:            quote.baseQuota,
		BonusQuota:           quote.bonusQuota,
		BonusPercent:         group.BonusPercent,
		TargetType:           "wallet",
		GroupBuyID:           group.GroupBuyID,
		GroupBuyMemberCount:  group.MemberCount,
	}
}

type monthlyPassConversionQuote struct {
	remainingQuota       int64
	planPriceAmount      float64
	unusedRatio          float64
	targetQuota          int
	baseQuota            int
	bonusQuota           int
	conversionPercent    int
	maxConversionPercent int
}

// ConvertMonthlyPassToUnifiedCredit cashes out a selected whole-pass percentage.
func ConvertMonthlyPassToUnifiedCredit(requestID string, userID int, subscriptionID int, conversionPercent int) (*commerceschema.SubscriptionClaudeConversionResult, error) {
	config := GetSubscriptionClaudeConversionConfig()
	if !config.Enabled {
		return nil, commerceschema.ErrSubscriptionClaudeConversionDisabled
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" || len(requestID) > 128 || userID <= 0 || subscriptionID <= 0 || conversionPercent <= 0 || conversionPercent > 100 {
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
			if existing.UserId != userID || existing.UserSubscriptionId != subscriptionID || existing.ConversionPercent != conversionPercent {
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
		// A competing request may have committed while this transaction waited
		// on the subscription lock. Replay its immutable target before checking
		// the now-consumed subscription again.
		if err := tx.Where("request_id = ?", requestID).First(existing).Error; err == nil {
			if existing.UserId != userID || existing.UserSubscriptionId != subscriptionID || existing.ConversionPercent != conversionPercent {
				return commerceschema.ErrSubscriptionClaudeConversionInvalid
			}
			return loadExistingMonthlyPassConversionResult(tx, result, existing)
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		plan, err := getSubscriptionPlanRecordTx(tx, sub.PlanId)
		if err != nil || plan == nil {
			return commerceschema.ErrSubscriptionClaudeConversionNoTarget
		}
		now := platformruntime.GetTimestamp()
		if err := maybeResetUserSubscriptionWithPlanTx(tx, sub, plan, now); err != nil {
			return err
		}
		resetUsed, err := subscriptionHasResetOpportunityUsageTx(tx, sub.Id)
		if err != nil {
			return err
		}
		if resetUsed {
			return commerceschema.ErrSubscriptionClaudeConversionResetUsed
		}
		pricedPlan, err := legacyConversionPaidPlanTx(tx, plan, sub)
		if err != nil {
			return err
		}
		quote := calculateMonthlyPassConversionQuote(pricedPlan, sub, now, conversionPercent)
		group, err := monthlyPassConversionGroupTx(tx, sub)
		if err != nil {
			return err
		}
		applyMonthlyPassGroupBonus(&quote, group)
		if quote.targetQuota <= 0 {
			return commerceschema.ErrSubscriptionClaudeConversionNoTarget
		}
		if pending, err := monthlyPassHasPendingReservationTx(tx, sub.Id); err != nil {
			return err
		} else if pending {
			return commerceschema.ErrSubscriptionClaudeConversionInProgress
		}
		if err := consumeMonthlyPassConversionQuotaTx(tx, sub, quote.remainingQuota, requestID); err != nil {
			if errors.Is(err, billingdomain.ErrInsufficientBalance) {
				return commerceschema.ErrSubscriptionClaudeConversionNoTarget
			}
			return err
		}

		sub.AmountUsed += quote.remainingQuota
		ended := sub.AmountUsed >= sub.AmountTotal
		if ended {
			sub.AmountUsed = sub.AmountTotal
			if periodAmount := getSubscriptionPeriodAmount(plan, sub); periodAmount > 0 {
				sub.PeriodUsed = periodAmount
			}
			sub.Status = "cancelled"
			sub.EndTime = now
		}
		if err := tx.Save(sub).Error; err != nil {
			return err
		}
		if ended {
			downgradeGroup, err = downgradeUserGroupForSubscriptionTx(tx, sub, now)
			if err != nil {
				return err
			}
		}

		// Keep conversion value and compensation in separate native receipts and
		// funding lots. Neither is a new cash top-up or referral-paid principal.
		if !tx.Migrator().HasTable(&billingschema.FundingLot{}) {
			return errors.New("conversion funding attribution is unavailable")
		}
		var walletOwner identityschema.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&walletOwner, userID).Error; err != nil {
			return err
		}
		if err := billingapp.CreditUnifiedWalletQuotaTx(tx, userID, quote.baseQuota, monthlyPassWalletReceiptKey(requestID, "base"), "subscription_conversion_base"); err != nil {
			return err
		}
		policy, err := json.Marshal(group)
		if err != nil {
			return err
		}
		if err := tx.Model(&billingschema.BillingLedgerEntry{}).Where("idempotency_key = ?", monthlyPassWalletReceiptKey(requestID, "base")).Update("metadata", json.RawMessage(policy)).Error; err != nil {
			return err
		}
		if quote.bonusQuota > 0 {
			if err := billingapp.CreditUnifiedWalletQuotaTx(tx, userID, quote.bonusQuota, monthlyPassWalletReceiptKey(requestID, "gift"), "subscription_conversion_gift"); err != nil {
				return err
			}
		}

		record := &commerceschema.SubscriptionClaudeConversion{
			UserId:              userID,
			UserSubscriptionId:  sub.Id,
			RequestId:           requestID,
			Status:              commerceschema.SubscriptionClaudeConversionStatusCompleted,
			SourceQuota:         quote.remainingQuota,
			TargetQuota:         quote.targetQuota,
			PlanPriceAmount:     quote.planPriceAmount,
			UnusedRatio:         quote.unusedRatio,
			ConversionPercent:   conversionPercent,
			TargetType:          "wallet",
			BaseQuota:           quote.baseQuota,
			BonusQuota:          quote.bonusQuota,
			BonusPercent:        group.BonusPercent,
			GroupBuyID:          group.GroupBuyID,
			GroupBuyMemberCount: group.MemberCount,
			RatioNumerator:      0,
			RatioDenominator:    0,
		}
		if err := tx.Create(record).Error; err != nil {
			return err
		}

		result.Conversion = record
		result.TargetType = "wallet"
		result.BaseQuota = quote.baseQuota
		result.BonusQuota = quote.bonusQuota
		result.BonusPercent = group.BonusPercent
		result.GroupBuyID = group.GroupBuyID
		result.GroupBuyMemberCount = group.MemberCount
		result.SourceQuota = quote.remainingQuota
		result.TargetQuota = quote.targetQuota
		result.PlanPriceAmount = quote.planPriceAmount
		result.UnusedRatio = quote.unusedRatio
		result.ConversionPercent = conversionPercent
		result.RemainingRatioAfter = decimal.NewFromInt(sub.AmountTotal - sub.AmountUsed).Div(decimal.NewFromInt(sub.AmountTotal)).InexactFloat64()
		result.SubscriptionEnded = ended
		result.AmountUsedAfter = sub.AmountUsed
		result.PeriodUsedAfter = sub.PeriodUsed
		result.QuotaAfter, err = billingapp.GetUnifiedCreditBalanceTx(tx, userID)
		if err != nil {
			return err
		}
		return nil
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

func subscriptionHasResetOpportunityUsageTx(tx *gorm.DB, subscriptionID int) (bool, error) {
	if subscriptionID <= 0 {
		return false, nil
	}
	if tx == nil {
		tx = platformdb.DB
	}
	var count int64
	err := tx.Model(&commerceschema.SubscriptionResetOpportunityLedger{}).
		Where("related_user_id = ? AND change_type = ?", subscriptionID, commerceschema.SubscriptionResetOpportunityChangeUse).
		Count(&count).Error
	return count > 0, err
}

func monthlyPassWalletReceiptKey(requestID, component string) string {
	return fmt.Sprintf("monthly-pass-wallet-conversion:%x:%s", sha256.Sum256([]byte(requestID)), component)
}

// Native credit receipts identify the new wallet policy without reinterpreting
// historical wallet or credit-pack conversions or adding mutable policy columns.
func attachMonthlyPassConversionReceipts(tx *gorm.DB, items []commerceschema.SubscriptionClaudeConversion) error {
	keys := make([]string, 0, len(items)*2)
	for i := range items {
		items[i].BaseQuota = items[i].TargetQuota
		items[i].TargetType = "wallet"
		if items[i].TargetCreditPackSubscriptionId > 0 {
			items[i].TargetType = "credit_pack"
			continue
		}
		keys = append(keys, monthlyPassWalletReceiptKey(items[i].RequestId, "base"), monthlyPassWalletReceiptKey(items[i].RequestId, "gift"))
	}
	if len(keys) == 0 {
		return nil
	}
	var entries []billingschema.BillingLedgerEntry
	if err := tx.Where("idempotency_key IN ?", keys).Find(&entries).Error; err != nil {
		return err
	}
	byKey := make(map[string]billingschema.BillingLedgerEntry, len(entries))
	for _, entry := range entries {
		byKey[entry.IdempotencyKey] = entry
	}
	for i := range items {
		item := &items[i]
		if item.TargetCreditPackSubscriptionId > 0 {
			continue
		}
		base, hasBase := byKey[monthlyPassWalletReceiptKey(item.RequestId, "base")]
		gift, hasGift := byKey[monthlyPassWalletReceiptKey(item.RequestId, "gift")]
		if !hasBase && !hasGift {
			continue // Historical wallet conversion: its original amount has no new gift.
		}
		if !hasBase || base.Amount <= 0 || base.ReasonCode != "subscription_conversion_base" || base.ReferenceType != "user" || base.ReferenceID != fmt.Sprint(item.UserId) {
			return errors.New("conversion base receipt is inconsistent")
		}
		if hasGift && (gift.Amount <= 0 || gift.ReasonCode != "subscription_conversion_gift" || gift.AccountID != base.AccountID || gift.ReferenceType != base.ReferenceType || gift.ReferenceID != base.ReferenceID) {
			return errors.New("conversion gift receipt is inconsistent")
		}
		if base.Amount != int64(item.TargetQuota)-gift.Amount {
			return errors.New("conversion receipt amounts do not match")
		}
		item.BaseQuota, item.BonusQuota = int(base.Amount), int(gift.Amount)
		var policy monthlyPassConversionGroup
		if len(base.Metadata) > 0 {
			if err := json.Unmarshal(base.Metadata, &policy); err != nil {
				return err
			}
		}
		item.BonusPercent, item.GroupBuyID, item.GroupBuyMemberCount = policy.BonusPercent, policy.GroupBuyID, policy.MemberCount
	}
	return nil
}

func loadExistingMonthlyPassConversionResult(tx *gorm.DB, result *commerceschema.SubscriptionClaudeConversionResult, existing *commerceschema.SubscriptionClaudeConversion) error {
	items := []commerceschema.SubscriptionClaudeConversion{*existing}
	if err := attachMonthlyPassConversionReceipts(tx, items); err != nil {
		return err
	}
	*existing = items[0]
	sub := &commerceschema.UserSubscription{}
	if err := tx.Where("id = ?", existing.UserSubscriptionId).First(sub).Error; err != nil {
		return err
	}
	balance := 0
	if existing.TargetCreditPackSubscriptionId > 0 {
		var target commerceschema.UserSubscription
		if err := tx.Where("id = ? AND user_id = ?", existing.TargetCreditPackSubscriptionId, existing.UserId).First(&target).Error; err != nil {
			return err
		}
		balance = int(target.AmountTotal - target.AmountUsed)
	} else {
		var err error
		balance, err = billingapp.GetUnifiedCreditBalanceTx(tx, existing.UserId)
		if err != nil {
			return err
		}
	}
	result.Conversion = existing
	result.SourceQuota = existing.SourceQuota
	result.TargetQuota = existing.TargetQuota
	result.BaseQuota = existing.BaseQuota
	result.BonusQuota = existing.BonusQuota
	result.BonusPercent = existing.BonusPercent
	result.GroupBuyID = existing.GroupBuyID
	result.GroupBuyMemberCount = existing.GroupBuyMemberCount
	result.TargetType = existing.TargetType
	result.TargetCreditPackSubscriptionId = existing.TargetCreditPackSubscriptionId
	result.QuotaAfter = balance
	result.AmountUsedAfter = sub.AmountUsed
	result.PeriodUsedAfter = sub.PeriodUsed
	result.PlanPriceAmount = existing.PlanPriceAmount
	result.UnusedRatio = existing.UnusedRatio
	result.ConversionPercent = existing.ConversionPercent
	if sub.AmountTotal > 0 {
		result.RemainingRatioAfter = decimal.NewFromInt(sub.AmountTotal - sub.AmountUsed).Div(decimal.NewFromInt(sub.AmountTotal)).InexactFloat64()
	}
	result.SubscriptionEnded = sub.Status != "active"
	return nil
}

func calculateMonthlyPassConversionQuote(plan *commerceschema.SubscriptionPlan, sub *commerceschema.UserSubscription, now int64, conversionPercent int) monthlyPassConversionQuote {
	if plan == nil || sub == nil || sub.Status != "active" || sub.EndTime <= now || sub.CreditPackGrantedQuota > 0 || plan.PlanType == commerceschema.SubscriptionPlanTypeCreditPack || plan.PriceAmount <= 0 || sub.AmountTotal <= 0 {
		return monthlyPassConversionQuote{}
	}
	remaining := sub.AmountTotal - sub.AmountUsed
	if remaining <= 0 {
		return monthlyPassConversionQuote{}
	}
	unusedRatio := decimal.NewFromInt(remaining).Div(decimal.NewFromInt(sub.AmountTotal))
	maxPercent := unusedRatio.Mul(decimal.NewFromInt(100)).Floor().IntPart()
	if maxPercent <= 0 {
		maxPercent = 1
	}
	if conversionPercent == 0 {
		conversionPercent = int(maxPercent)
	}
	if conversionPercent <= 0 || int64(conversionPercent) > maxPercent {
		return monthlyPassConversionQuote{}
	}
	conversionRatio := decimal.NewFromInt(int64(conversionPercent)).Div(decimal.NewFromInt(100))
	convertedQuota := decimal.NewFromInt(sub.AmountTotal).Mul(conversionRatio).Floor().IntPart()
	convertAllRemaining := int64(conversionPercent) == maxPercent
	if convertAllRemaining {
		conversionRatio = unusedRatio
		convertedQuota = remaining
	}
	if convertedQuota <= 0 || convertedQuota > remaining {
		return monthlyPassConversionQuote{}
	}
	target := decimal.NewFromFloat(plan.PriceAmount).
		Mul(conversionRatio).
		Mul(decimal.NewFromFloat(platformruntime.QuotaPerUnit)).
		Floor().
		IntPart()
	if target <= 0 && convertAllRemaining {
		target = 1
	}
	if target <= 0 {
		return monthlyPassConversionQuote{}
	}
	// No fixed compensation applies. Historical gifts are read from immutable
	// credit receipts instead of being recomputed from current policy.
	bonus := int64(0)
	return monthlyPassConversionQuote{
		remainingQuota:       convertedQuota,
		planPriceAmount:      plan.PriceAmount,
		unusedRatio:          unusedRatio.InexactFloat64(),
		targetQuota:          int(target + bonus),
		baseQuota:            int(target),
		bonusQuota:           int(bonus),
		conversionPercent:    conversionPercent,
		maxConversionPercent: int(maxPercent),
	}
}

func consumeMonthlyPassConversionQuotaTx(tx *gorm.DB, sub *commerceschema.UserSubscription, amount int64, requestID string) error {
	account, err := billingdomain.EnsureBillingAccountTx(tx, billingdomain.EnsureAccountParams{
		AccountType: "subscription",
		OwnerType:   "user_subscription",
		OwnerID:     int64(sub.Id),
		QuotaUnit:   "quota",
	})
	if err != nil {
		return err
	}
	ledgerBacked, err := subscriptionLedgerHasEntriesTx(tx, account.AccountID)
	if err != nil {
		return err
	}
	if !ledgerBacked {
		available := sub.AmountTotal - sub.AmountUsed
		if available > 0 {
			_, err = billingdomain.CreditAccountTx(tx, billingdomain.CreditAccountParams{
				AccountID:      account.AccountID,
				Amount:         available,
				IdempotencyKey: fmt.Sprintf("subscription-bootstrap:%d", sub.Id),
				ReasonCode:     "subscription_balance_bootstrap",
				ReferenceType:  "user_subscription",
				ReferenceID:    fmt.Sprintf("%d", sub.Id),
				OperatorType:   "monthly_pass_conversion",
				OperatorID:     requestID,
			})
			if err != nil {
				return err
			}
		}
	}
	reservation, err := billingdomain.CreateReservationTx(tx, billingdomain.CreateReservationParams{
		AccountID:      account.AccountID,
		RequestID:      fmt.Sprintf("monthly-pass-conversion:%x", sha256.Sum256([]byte(requestID)))[:64],
		ReservedAmount: amount,
		IdempotencyKey: "monthly-pass-conversion:" + requestID + ":reserve",
	})
	if err != nil {
		return err
	}
	_, err = billingdomain.SettleReservationTx(tx, billingdomain.SettleReservationParams{
		ReservationID:  reservation.ReservationID,
		ActualAmount:   amount,
		IdempotencyKey: "monthly-pass-conversion:" + requestID + ":settle",
	})
	return err
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

	// A conversion reserves and settles within one transaction. An open
	// conversion reservation therefore means a previous request was interrupted
	// after the hold was committed. Recover it before retrying instead of
	// permanently blocking the subscription on a stale snapshot balance.
	var stale []billingschema.BillingReservation
	err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("account_id = ? AND status = ? AND idempotency_key LIKE ?", account.AccountID, billingschema.BillingReservationStatusOpen, "monthly-pass-conversion:%:reserve").
		Find(&stale).Error
	if err != nil {
		return false, err
	}
	for _, reservation := range stale {
		if _, err := billingdomain.ReleaseReservationTx(tx, billingdomain.ReleaseReservationParams{
			ReservationID:  reservation.ReservationID,
			IdempotencyKey: "monthly-pass-conversion-recovery:" + reservation.ReservationID,
			ReasonCode:     "monthly_pass_conversion_recovery",
		}); err != nil {
			return false, err
		}
	}
	return false, nil
}
