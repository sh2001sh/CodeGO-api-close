package app

import (
	"errors"
	"fmt"
	"strings"

	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CreditSubscriptionQuotaTx restores quota to a subscription and its ledger
// projection in one idempotent transaction. The negative usage delta keeps the
// legacy projection aligned with the ledger-backed available balance.
func CreditSubscriptionQuotaTx(tx *gorm.DB, subscriptionID int, modelName string, amount int64, idempotencyKey, reasonCode string) error {
	if tx == nil {
		return errors.New("transaction is required")
	}
	if subscriptionID <= 0 || amount <= 0 || strings.TrimSpace(idempotencyKey) == "" {
		return errors.New("subscription id, amount and idempotency key are required")
	}

	var existing billingschema.BillingLedgerEntry
	lookup := tx.Where("idempotency_key = ?", idempotencyKey).First(&existing)
	if lookup.Error == nil {
		if existing.Amount != amount || existing.EntryType != billingdomain.LedgerEntryTypeGrantCredit {
			return billingdomain.ErrLedgerConflict
		}
		return nil
	}
	if !errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
		return lookup.Error
	}

	sub := &commerceschema.UserSubscription{}
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", subscriptionID).First(sub).Error; err != nil {
		return err
	}
	plan, err := getSubscriptionPlanRecordTx(tx, sub.PlanId)
	if err != nil {
		return err
	}
	if err := applySubscriptionUsageDelta(plan, sub, modelName, -amount); err != nil {
		return err
	}
	account, err := billingdomain.EnsureBillingAccountTx(tx, billingdomain.EnsureAccountParams{
		AccountType: "subscription", OwnerType: "user_subscription", OwnerID: int64(subscriptionID), QuotaUnit: "quota",
	})
	if err != nil {
		return err
	}
	if _, err := billingdomain.CreditAccountTx(tx, billingdomain.CreditAccountParams{
		AccountID: account.AccountID, Amount: amount, IdempotencyKey: idempotencyKey,
		ReasonCode: reasonCode, ReasonDetail: "marketplace multiplier correction",
		ReferenceType: "marketplace_settlement", ReferenceID: idempotencyKey,
		OperatorType: "system", OperatorID: "marketplace-multiplier-correction",
	}); err != nil {
		return err
	}
	if err := tx.Save(sub).Error; err != nil {
		return fmt.Errorf("save subscription projection: %w", err)
	}
	return nil
}
