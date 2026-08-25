package app

import (
	"fmt"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
)

// GetUserLedgerConsumedQuota returns the total settled usage represented by
// the user's wallet and subscription billing accounts.
func GetUserLedgerConsumedQuota(userID int) (int64, error) {
	if userID <= 0 {
		return 0, fmt.Errorf("invalid user id")
	}

	// Keep the subscription lookup in SQL so billing does not import the
	// commerce package and create an application-layer import cycle.
	subscriptionQuery := platformdb.DB.Table("user_subscriptions").
		Select("user_subscriptions.id").
		Where("user_subscriptions.user_id = ?", userID)

	accountTable := billingschema.BillingAccount{}.TableName()
	accountQuery := platformdb.DB.Model(&billingschema.BillingAccount{}).
		Select(accountTable+".account_id").
		Where(
			fmt.Sprintf("(%s.owner_type = ? AND %s.owner_id = ? AND %s.account_type IN ?) OR (%s.owner_type = ? AND %s.owner_id IN (?) AND %s.account_type = ?)", accountTable, accountTable, accountTable, accountTable, accountTable, accountTable),
			"user", userID, []string{"wallet", "claude_wallet"},
			"user_subscription", subscriptionQuery, "subscription",
		)

	var accountIDs []string
	if err := accountQuery.Pluck(accountTable+".account_id", &accountIDs).Error; err != nil {
		return 0, err
	}
	if len(accountIDs) == 0 {
		return 0, nil
	}

	var consumed int64
	if err := platformdb.DB.Model(&billingschema.BillingBalanceSnapshot{}).
		Where("account_id IN ?", accountIDs).
		Select("COALESCE(SUM(consumed_total), 0)").
		Scan(&consumed).Error; err != nil {
		return 0, err
	}
	return consumed, nil
}

// GetUserHistoricalUsedQuota prefers the ledger balance projection and
// falls back to the legacy counter for users without a complete ledger.
// Taking the larger value avoids double counting while old and new billing
// paths are being reconciled.
func GetUserHistoricalUsedQuota(userID int, legacyUsedQuota int) (int, error) {
	ledgerConsumed, err := GetUserLedgerConsumedQuota(userID)
	if err != nil {
		return 0, err
	}
	if ledgerConsumed > int64(legacyUsedQuota) {
		return int(ledgerConsumed), nil
	}
	return legacyUsedQuota, nil
}
