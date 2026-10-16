package store

import (
	"testing"

	"github.com/glebarez/sqlite"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestReferralChannelFundingMigrationPreservesHistoryAndRetries(t *testing.T) {
	oldPG := platformdb.UsingPostgreSQL
	platformdb.UsingPostgreSQL = false
	t.Cleanup(func() { platformdb.UsingPostgreSQL = oldPG })
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	marketTable := (marketplaceschema.Settlement{}).TableName()
	receiptTable := (billingschema.ReferralConsumptionRebate{}).TableName()
	require.NoError(t, db.Exec("CREATE TABLE "+marketTable+" (id text primary key, owner_net_amount bigint, status text)").Error)
	require.NoError(t, db.Exec("CREATE TABLE "+receiptTable+" (settlement_id text primary key, rebate_quota bigint)").Error)
	require.NoError(t, db.Table(marketTable).Create(map[string]any{
		"id": "historical", "owner_net_amount": 9850, "status": "pending",
	}).Error)
	require.NoError(t, db.Table(receiptTable).Create(map[string]any{
		"settlement_id": "historical", "rebate_quota": 300,
	}).Error)
	require.NoError(t, db.Transaction(migrateReferralChannelFunding))
	var item marketplaceschema.Settlement
	require.NoError(t, db.First(&item, "id = ?", "historical").Error)
	require.EqualValues(t, 9850, item.OwnerNetAmount)
	require.Equal(t, "pending", item.Status)
	require.Zero(t, item.ReferralRebateAmount)
	require.False(t, item.ReferralRebatePending, "historical earnings must remain releasable")
	var receipt billingschema.ReferralConsumptionRebate
	require.NoError(t, db.First(&receipt, "settlement_id = ?", "historical").Error)
	require.EqualValues(t, 300, receipt.RebateQuota)
	require.Empty(t, receipt.FundingSource)
	require.Empty(t, receipt.MarketplaceSettlementID)

	require.NoError(t, db.Model(&item).Updates(map[string]any{
		"owner_net_amount": 9550, "referral_rebate_amount": 300, "referral_rebate_pending": true,
	}).Error)
	require.NoError(t, db.Model(&receipt).Updates(map[string]any{
		"funding_source": "marketplace_channel", "marketplace_settlement_id": "historical",
	}).Error)
	for range 2 {
		require.NoError(t, db.Transaction(migrateReferralChannelFunding))
	}
	require.NoError(t, db.First(&item, "id = ?", "historical").Error)
	require.EqualValues(t, 9550, item.OwnerNetAmount)
	require.EqualValues(t, 300, item.ReferralRebateAmount)
	require.True(t, item.ReferralRebatePending)
	require.NoError(t, db.First(&receipt, "settlement_id = ?", "historical").Error)
	require.EqualValues(t, 300, receipt.RebateQuota)
	require.Equal(t, "marketplace_channel", receipt.FundingSource)
	require.Equal(t, "historical", receipt.MarketplaceSettlementID)
	require.Contains(t, V2MigrationIDs(), "20261016_referral_channel_funding")
}
