package store

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMigrateRemoveBountyMarketDropsOnlyRetiredTables(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)

	bountyTables := []string{
		"bounty_material_replies",
		"bounty_material_requests",
		"bounty_applications",
		"bounty_submissions",
		"bounty_disputes",
		"bounty_notifications",
		"bounty_events",
		"bounty_reports",
		"bounty_tasks",
	}
	for _, table := range bountyTables {
		require.NoError(t, db.Exec("CREATE TABLE "+table+" (id integer primary key)").Error)
	}
	require.NoError(t, db.Exec("CREATE TABLE billing_sentinel (id integer primary key)").Error)

	require.NoError(t, migrateRemoveBountyMarket(db))
	require.NoError(t, migrateRemoveBountyMarket(db))

	for _, table := range bountyTables {
		require.False(t, db.Migrator().HasTable(table), table)
	}
	require.True(t, db.Migrator().HasTable("billing_sentinel"))
}
