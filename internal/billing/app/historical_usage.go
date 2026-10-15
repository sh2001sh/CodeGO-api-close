package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
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

var historicalUsageDisplayRefreshSlots = make(chan struct{}, 1)

const historicalUsageTTL = 30 * time.Second

const historicalUsageDisplayTimeout = 10 * time.Second

// GetUserHistoricalUsedQuotaForDisplay serves the last aggregate (or the
// existing usage counter on a cold cache) while refreshing it asynchronously.
// A profile must not wait for a scan of millions of historical settlements.
// Financial API callers continue to use GetUserHistoricalUsedQuota below.
func GetUserHistoricalUsedQuotaForDisplay(userID int, legacyUsedQuota int) (int, error) {
	if userID <= 0 {
		return 0, fmt.Errorf("invalid user id")
	}
	key := fmt.Sprintf("%p:%d", platformdb.DB, userID)
	historicalUsageCache.Lock()
	cached, ok := historicalUsageCache.items[key]
	historicalUsageCache.Unlock()
	if !ok || time.Since(cached.at) >= historicalUsageTTL {
		startHistoricalUsageDisplayRefresh(userID, key)
	}
	if cached.amount > int64(legacyUsedQuota) {
		return int(cached.amount), nil
	}
	return legacyUsedQuota, nil
}

func startHistoricalUsageDisplayRefresh(userID int, key string) {
	select {
	case historicalUsageDisplayRefreshSlots <- struct{}{}:
	default:
		return
	}
	go func() {
		defer func() { <-historicalUsageDisplayRefreshSlots }()
		ctx, cancel := context.WithTimeout(context.Background(), historicalUsageDisplayTimeout)
		defer cancel()
		amount, err := loadUserLedgerConsumedQuotaContext(ctx, userID)
		if err != nil {
			platformobservability.SysError("refresh profile historical usage: " + err.Error())
			return
		}
		cacheHistoricalUsage(key, amount)
	}()
}

func cacheHistoricalUsage(key string, amount int64) {
	historicalUsageCache.Lock()
	defer historicalUsageCache.Unlock()
	if len(historicalUsageCache.items) >= 1024 {
		for itemKey, value := range historicalUsageCache.items {
			if time.Since(value.at) >= historicalUsageTTL {
				delete(historicalUsageCache.items, itemKey)
			}
		}
		if len(historicalUsageCache.items) >= 1024 {
			clear(historicalUsageCache.items)
		}
	}
	historicalUsageCache.items[key] = historicalUsageEntry{amount: amount, at: time.Now()}
}

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
		amount, err := loadUserLedgerConsumedQuotaContext(context.Background(), userID)
		if err != nil {
			return nil, err
		}
		cacheHistoricalUsage(key, amount)
		return amount, nil
	})
	if err != nil {
		return 0, err
	}
	return value.(int64), nil
}

func loadUserLedgerConsumedQuotaContext(ctx context.Context, userID int) (int64, error) {
	db := platformdb.DB.WithContext(ctx)

	// Keep the subscription lookup in SQL so billing does not import the
	// commerce package and create an application-layer import cycle.
	subscriptionQuery := db.Table("user_subscriptions").
		Select("user_subscriptions.id").
		Where("user_subscriptions.user_id = ?", userID)

	accountTable := billingschema.BillingAccount{}.TableName()
	accountQuery := db.Model(&billingschema.BillingAccount{}).
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
	if err := db.Model(&billingschema.BillingSettlement{}).
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
