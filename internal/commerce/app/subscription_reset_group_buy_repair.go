package app

import (
	"errors"
	"fmt"
	"strings"
	"time"

	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const subscriptionResetGroupBuyRepairLookback = 90 * 24 * time.Hour

type subscriptionResetGroupBuyRepairResult struct {
	Scanned       int
	Repaired      int
	Skipped       int
	RestoredQuota int64
}

// RepairRecentSubscriptionResetGroupBuyBonuses restores group-buy quota omitted
// by the old reset implementation. Every repair is ledger-idempotent and only
// uses reset rows whose historical balance can be reconstructed exactly.
func RepairRecentSubscriptionResetGroupBuyBonuses() (subscriptionResetGroupBuyRepairResult, error) {
	result := subscriptionResetGroupBuyRepairResult{}
	if platformdb.DB == nil {
		return result, nil
	}
	var ledgers []commerceschema.SubscriptionResetOpportunityLedger
	if err := platformdb.DB.Where("change_type = ? AND created_at >= ?", commerceschema.SubscriptionResetOpportunityChangeUse, time.Now().Add(-subscriptionResetGroupBuyRepairLookback).Unix()).
		Order("created_at ASC, id ASC").Find(&ledgers).Error; err != nil {
		return result, err
	}
	for _, ledger := range ledgers {
		result.Scanned++
		restored, reason, err := repairSubscriptionResetGroupBuyBonus(ledger)
		if err != nil {
			return result, fmt.Errorf("repair reset ledger %d: %w", ledger.Id, err)
		}
		if restored <= 0 {
			result.Skipped++
			if reason != "" && reason != "no_missing_quota" && reason != "already_repaired" {
				platformobservability.SysLog(fmt.Sprintf("subscription reset group-buy repair skipped ledger_id=%d subscription_id=%d reason=%s", ledger.Id, ledger.RelatedUserId, reason))
			}
			continue
		}
		result.Repaired++
		result.RestoredQuota += restored
	}
	platformobservability.SysLog(fmt.Sprintf("subscription reset group-buy repair completed scanned=%d repaired=%d skipped=%d restored_quota=%d", result.Scanned, result.Repaired, result.Skipped, result.RestoredQuota))
	return result, nil
}

func repairSubscriptionResetGroupBuyBonus(resetLedger commerceschema.SubscriptionResetOpportunityLedger) (int64, string, error) {
	restored := int64(0)
	reason := ""
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var sub commerceschema.UserSubscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", resetLedger.RelatedUserId).First(&sub).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				reason = "subscription_missing"
				return nil
			}
			return err
		}
		if sub.StartTime > resetLedger.CreatedAt {
			reason = "subscription_renewed"
			return nil
		}

		account, err := billingdomain.EnsureBillingAccountTx(tx, billingdomain.EnsureAccountParams{
			AccountType: "subscription", OwnerType: "user_subscription", OwnerID: int64(sub.Id), QuotaUnit: "quota",
		})
		if err != nil {
			return err
		}
		repairKey := fmt.Sprintf("subscription-reset-group-buy-repair:%d", resetLedger.Id)
		var existing billingschema.BillingLedgerEntry
		if err := tx.Where("idempotency_key = ?", repairKey).First(&existing).Error; err == nil {
			reason = "already_repaired"
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		resetKey := fmt.Sprintf("subscription-reset-balance:%d:opportunity:%d:%s", sub.Id, resetLedger.UserId, resetLedger.UsedMonth)
		var resetEntry billingschema.BillingLedgerEntry
		if err := tx.Where("account_id = ? AND idempotency_key = ?", account.AccountID, resetKey).First(&resetEntry).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				reason = "reset_balance_entry_missing"
				return nil
			}
			return err
		}
		if resetEntry.BalanceAfter == nil {
			reason = "reset_balance_missing"
			return nil
		}

		bonusQuota, err := currentCycleGroupBuyBonusQuotaTx(tx, sub.Id, sub.StartTime, resetLedger.CreatedAt)
		if err != nil {
			return err
		}
		if bonusQuota <= 0 {
			reason = "no_group_buy_bonus"
			return nil
		}

		var laterGrants []billingschema.BillingLedgerEntry
		if err := tx.Where("account_id = ? AND created_at > ? AND entry_type = ?", account.AccountID, resetEntry.CreatedAt, billingdomain.LedgerEntryTypeGrantCredit).
			Find(&laterGrants).Error; err != nil {
			return err
		}
		laterProjectionGrants := int64(0)
		for _, grant := range laterGrants {
			switch grant.ReasonCode {
			case "subscription_bonus", "subscription_fuel", "blind_box_subscription":
				laterProjectionGrants += grant.Amount
			case "subscription_reset_group_buy_repair":
				// Idempotency is checked above; another repair does not change AmountTotal.
			default:
				reason = "later_grant_requires_manual_review"
				return nil
			}
		}
		totalAtReset := sub.AmountTotal - laterProjectionGrants
		if totalAtReset <= 0 {
			reason = "historical_total_unavailable"
			return nil
		}

		var reservedAtReset int64
		if err := tx.Model(&billingschema.BillingReservation{}).
			Where("account_id = ? AND created_at <= ? AND (updated_at > ? OR status = ?)", account.AccountID, resetEntry.CreatedAt, resetEntry.CreatedAt, billingschema.BillingReservationStatusOpen).
			Select("COALESCE(SUM(reserved_amount), 0)").Scan(&reservedAtReset).Error; err != nil {
			return err
		}
		usedAfterReset := totalAtReset - *resetEntry.BalanceAfter - reservedAtReset
		if usedAfterReset < 0 {
			reason = "historical_balance_inconsistent"
			return nil
		}
		missing := min(bonusQuota, usedAfterReset)
		missing = min(missing, sub.AmountUsed)
		if missing <= 0 {
			reason = "no_missing_quota"
			return nil
		}

		if _, err := billingdomain.CreditAccountTx(tx, billingdomain.CreditAccountParams{
			AccountID: account.AccountID, Amount: missing, IdempotencyKey: repairKey,
			ReasonCode: "subscription_reset_group_buy_repair", ReasonDetail: "restore group-buy quota omitted by subscription reset",
			ReferenceType: "subscription_reset_opportunity", ReferenceID: fmt.Sprintf("%d", resetLedger.Id),
			OperatorType: "system_repair", OperatorID: strings.TrimSpace(resetLedger.EventKey),
		}); err != nil {
			return err
		}
		if err := tx.Model(&sub).Update("amount_used", gorm.Expr("amount_used - ?", missing)).Error; err != nil {
			return err
		}
		restored = missing
		return nil
	})
	if err == nil && restored > 0 {
		platformobservability.SysLog(fmt.Sprintf("subscription reset group-buy repaired ledger_id=%d user_id=%d subscription_id=%d restored_quota=%d", resetLedger.Id, resetLedger.UserId, resetLedger.RelatedUserId, restored))
	}
	return restored, reason, err
}
