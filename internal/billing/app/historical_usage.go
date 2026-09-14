package app

import (
	"fmt"
	"sync"
	"time"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"golang.org/x/sync/singleflight"
)

// This aggregate is display-only; balances and authorization continue to read
// their authoritative ledgers. Do not rescan a user's entire settlement history
// every time another layout component requests the profile.
var historicalUsageCache = struct {
	sync.Mutex
	items map[string]historicalUsageEntry
}{items: make(map[string]historicalUsageEntry)}

type historicalUsageEntry struct {
	amount int64
	at     time.Time
}

var historicalUsageLoads singleflight.Group

const historicalUsageTTL = 30 * time.Second

// GetUserLedgerConsumedQuota returns request-backed settled usage from the
// user's wallet and subscription billing accounts. Non-request balance moves
// such as migrations, transfers, blind-box purchases, and conversions are not
// user API consumption and are intentionally excluded.
func GetUserLedgerConsumedQuota(userID int) (int64, error) {
	if userID <= 0 {
		return 0, fmt.Errorf("invalid user id")
	}
	key := fmt.Sprintf("%p:%d", platformdb.DB, userID)
	value, err, _ := historicalUsageLoads.Do(key, func() (any, error) {
		historicalUsageCache.Lock()
		cached, ok := historicalUsageCache.items[key]
		historicalUsageCache.Unlock()
		if ok && time.Since(cached.at) < historicalUsageTTL {
			return cached.amount, nil
		}
		amount, err := loadUserLedgerConsumedQuota(userID)
		if err != nil {
			return nil, err
		}
		historicalUsageCache.Lock()
		defer historicalUsageCache.Unlock()
		if len(historicalUsageCache.items) >= 1024 {
			for key, value := range historicalUsageCache.items {
				if time.Since(value.at) >= historicalUsageTTL {
					delete(historicalUsageCache.items, key)
				}
			}
			if len(historicalUsageCache.items) >= 1024 {
				clear(historicalUsageCache.items)
			}
		}
		historicalUsageCache.items[key] = historicalUsageEntry{amount: amount, at: time.Now()}
		return amount, nil
	})
	if err != nil {
		return 0, err
	}
	return value.(int64), nil
}

func loadUserLedgerConsumedQuota(userID int) (int64, error) {

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

	settlementTable := billingschema.BillingSettlement{}.TableName()
	reservationTable := billingschema.BillingReservation{}.TableName()
	var consumed int64
	if err := platformdb.DB.Model(&billingschema.BillingSettlement{}).
		Joins("JOIN "+reservationTable+" AS usage_reservations ON usage_reservations.reservation_id = "+settlementTable+".reservation_id").
		Where("usage_reservations.account_id IN ?", accountIDs).
		Where("usage_reservations.status = ?", billingschema.BillingReservationStatusSettled).
		Where(settlementTable+".status = ?", billingschema.BillingSettlementStatusCompleted).
		Where(settlementTable+".usage_evidence_id <> ?", "").
		Where(settlementTable+".idempotency_key NOT LIKE ?", "monthly-pass-conversion:%").
		Select("COALESCE(SUM(" + settlementTable + ".actual_amount), 0)").
		Scan(&consumed).Error; err != nil {
		return 0, err
	}
	return consumed, nil
}

// GetUserHistoricalUsedQuota prefers request-backed ledger settlements and
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
