package app

import (
	"fmt"
	"testing"
	"time"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
)

func TestHistoricalUsageCacheIsScopedAndExpires(t *testing.T) {
	truncate(t)
	seedUser(t, 9501, 0)
	account := &billingschema.BillingAccount{AccountID: "wallet-cache-history", AccountType: "wallet", OwnerType: "user", OwnerID: 9501, QuotaUnit: "quota", Status: "active"}
	require.NoError(t, platformdb.DB.Create(account).Error)
	createHistoricalUsageSettlement(t, account.AccountID, "cached-usage", 40, true)
	amount, err := GetUserLedgerConsumedQuota(9501)
	require.NoError(t, err)
	require.Equal(t, int64(40), amount)
	createHistoricalUsageSettlement(t, account.AccountID, "new-usage", 20, true)
	amount, err = GetUserLedgerConsumedQuota(9501)
	require.NoError(t, err)
	require.Equal(t, int64(40), amount, "repeated profile reads reuse the aggregate")
	other, err := GetUserLedgerConsumedQuota(9502)
	require.NoError(t, err)
	require.Zero(t, other, "another user must not see the cached usage")
	key := fmt.Sprintf("%p:%d", platformdb.DB, 9501)
	historicalUsageCache.Lock()
	historicalUsageCache.items[key] = historicalUsageEntry{amount: 40, at: time.Now().Add(-historicalUsageTTL)}
	historicalUsageCache.Unlock()
	amount, err = GetUserLedgerConsumedQuota(9501)
	require.NoError(t, err)
	require.Equal(t, int64(60), amount)
	_, err = GetUserLedgerConsumedQuota(0)
	require.Error(t, err)
}

func TestGetUserLedgerConsumedQuotaIncludesWalletAndSubscriptions(t *testing.T) {
	truncate(t)
	seedUser(t, 9101, 0)
	require.NoError(t, platformdb.DB.Create(&commerceschema.UserSubscription{Id: 9201, UserId: 9101, Status: "active"}).Error)

	wallet := &billingschema.BillingAccount{AccountID: "wallet-history", AccountType: "wallet", OwnerType: "user", OwnerID: 9101, QuotaUnit: "quota", Status: "active"}
	subscription := &billingschema.BillingAccount{AccountID: "subscription-history", AccountType: "subscription", OwnerType: "user_subscription", OwnerID: 9201, QuotaUnit: "quota", Status: "active"}
	require.NoError(t, platformdb.DB.Create(wallet).Error)
	require.NoError(t, platformdb.DB.Create(subscription).Error)
	require.NoError(t, platformdb.DB.Create(&billingschema.BillingBalanceSnapshot{AccountID: wallet.AccountID, ConsumedTotal: 40}).Error)
	require.NoError(t, platformdb.DB.Create(&billingschema.BillingBalanceSnapshot{AccountID: subscription.AccountID, ConsumedTotal: 240}).Error)
	createHistoricalUsageSettlement(t, wallet.AccountID, "wallet-usage", 40, true)
	createHistoricalUsageSettlement(t, subscription.AccountID, "subscription-usage", 240, true)

	consumed, err := GetUserLedgerConsumedQuota(9101)
	require.NoError(t, err)
	require.Equal(t, int64(280), consumed)
	historical, err := GetUserHistoricalUsedQuota(9101, 100)
	require.NoError(t, err)
	require.Equal(t, 280, historical)
}

func TestHistoricalUsageKeepsLegacyCounterForUsersWithoutLedgerAccounts(t *testing.T) {
	truncate(t)
	testUser := &identityschema.User{Id: 9301, Username: "legacy-history", UsedQuota: 300}
	require.NoError(t, platformdb.DB.Create(testUser).Error)

	got, err := GetUserHistoricalUsedQuota(testUser.Id, testUser.UsedQuota)
	require.NoError(t, err)
	require.Equal(t, 300, got)
}

func TestHistoricalUsageExcludesNonRequestSettlements(t *testing.T) {
	truncate(t)
	seedUser(t, 9401, 0)

	account := &billingschema.BillingAccount{
		AccountID:   "wallet-migration-history",
		AccountType: "wallet",
		OwnerType:   "user",
		OwnerID:     9401,
		QuotaUnit:   "quota",
		Status:      "active",
	}
	require.NoError(t, platformdb.DB.Create(account).Error)
	require.NoError(t, platformdb.DB.Create(&billingschema.BillingBalanceSnapshot{
		AccountID:     account.AccountID,
		ConsumedTotal: 100,
	}).Error)
	createHistoricalUsageSettlement(t, account.AccountID, "actual-usage", 60, true)
	createHistoricalUsageSettlement(t, account.AccountID, "unified-credit-migration", 40, false)
	createHistoricalUsageSettlement(t, account.AccountID, "monthly-pass-conversion:legacy", 20, true)

	consumed, err := GetUserLedgerConsumedQuota(9401)
	require.NoError(t, err)
	require.Equal(t, int64(60), consumed)
}

func createHistoricalUsageSettlement(t *testing.T, accountID, id string, amount int64, withEvidence bool) {
	t.Helper()
	reservation := &billingschema.BillingReservation{
		ReservationID:  id + "-reservation",
		RequestID:      id + "-request",
		AccountID:      accountID,
		ReservedAmount: amount,
		Status:         billingschema.BillingReservationStatusSettled,
		IdempotencyKey: id + "-reservation-idempotency",
	}
	require.NoError(t, platformdb.DB.Create(reservation).Error)
	evidenceID := ""
	if withEvidence {
		evidenceID = id + "-evidence"
	}
	require.NoError(t, platformdb.DB.Create(&billingschema.BillingSettlement{
		SettlementID:    id + "-settlement",
		ReservationID:   reservation.ReservationID,
		UsageEvidenceID: evidenceID,
		ActualAmount:    amount,
		Status:          billingschema.BillingSettlementStatusCompleted,
		IdempotencyKey:  id + ":settle",
	}).Error)
}
