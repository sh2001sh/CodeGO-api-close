package store

import (
	"os"
	"testing"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestSettlementBacklogIndexesPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_LEDGER_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set TEST_LEDGER_POSTGRES_DSN to a dedicated test database")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	oldDB, oldPG, oldSQLite := platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite
	platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite = db, true, false
	t.Cleanup(func() {
		platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite = oldDB, oldPG, oldSQLite
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.Exec("CREATE SCHEMA IF NOT EXISTS billing").Error)
	require.NoError(t, db.Exec("CREATE SCHEMA IF NOT EXISTS marketplace").Error)
	require.NoError(t, db.AutoMigrate(&billingschema.BillingOutboxEvent{}, &marketplaceschema.Settlement{}))
	require.NoError(t, migrateSettlementBacklogIndexes(db))
	require.NoError(t, migrateMarketplaceReferralBacklogIndex(db))
	require.NoError(t, migrateMarketplaceReleaseOwnerIndex(db))

	for _, name := range []string{
		"idx_marketplace_settlements_releasable",
		"idx_billing_outbox_pending_referral",
		"idx_billing_outbox_pending_referral_account",
		"idx_billing_outbox_pending_marketplace_referral",
		"idx_marketplace_settlements_releasable_owner",
	} {
		var count int64
		require.NoError(t, db.Raw(`SELECT count(*) FROM pg_indexes WHERE indexname = ?`, name).Scan(&count).Error)
		require.EqualValues(t, 1, count, name)
	}
}
