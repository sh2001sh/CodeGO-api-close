package store

import (
	"testing"

	"github.com/glebarez/sqlite"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCreditPackPolicyUpgradePreservesLegacyRowsAndDefaults(t *testing.T) {
	oldPG := platformdb.UsingPostgreSQL
	platformdb.UsingPostgreSQL = false
	t.Cleanup(func() { platformdb.UsingPostgreSQL = oldPG })
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	for _, table := range []string{"subscription_orders", "user_subscriptions", "subscription_claude_conversions", "channels", "marketplace_channels", "marketplace_settlements"} {
		require.NoError(t, db.Exec("CREATE TABLE "+table+" (id INTEGER PRIMARY KEY, legacy_amount INTEGER NOT NULL DEFAULT 0)").Error)
		require.NoError(t, db.Exec("INSERT INTO "+table+" (id, legacy_amount) VALUES (1, 123)").Error)
	}
	for range 2 {
		require.NoError(t, db.Transaction(migrateCreditPackFundingPolicy))
	}
	for _, table := range []string{"subscription_orders", "user_subscriptions", "subscription_claude_conversions", "channels", "marketplace_channels", "marketplace_settlements"} {
		var amount int
		require.NoError(t, db.Table(table).Where("id = ?", 1).Pluck("legacy_amount", &amount).Error)
		require.Equal(t, 123, amount)
	}
	var subscription commerceschema.UserSubscription
	require.NoError(t, db.First(&subscription, 1).Error)
	require.Zero(t, subscription.CreditPackGrantedQuota, "legacy pricing must remain distinguishable after migration")
	var marketChannel marketplaceschema.Channel
	require.NoError(t, db.Unscoped().First(&marketChannel, "id = ?", 1).Error)
	require.False(t, marketChannel.CreditPackSupported)
	var channel gatewayschema.Channel
	require.NoError(t, db.Unscoped().First(&channel, 1).Error)
	require.False(t, channel.CreditPackSupported)
	var settlement marketplaceschema.Settlement
	require.NoError(t, db.First(&settlement, "id = ?", 1).Error)
	require.Zero(t, settlement.CreditPackChannelShareBPS)
	require.Zero(t, settlement.CreditPackDiscountAmount)
}
