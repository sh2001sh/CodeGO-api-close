package app

import (
	"testing"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
)

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
