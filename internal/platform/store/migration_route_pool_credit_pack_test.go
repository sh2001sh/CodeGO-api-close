package store

import (
	"testing"

	"github.com/glebarez/sqlite"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRoutePoolCreditPackMigrationPreservesSavedPools(t *testing.T) {
	oldPG := platformdb.UsingPostgreSQL
	platformdb.UsingPostgreSQL = false
	t.Cleanup(func() { platformdb.UsingPostgreSQL = oldPG })
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	table := (marketplaceschema.RoutePool{}).TableName()
	require.NoError(t, db.Exec("CREATE TABLE "+table+" (id varchar(64) primary key, name text, auto_build_enabled boolean not null default false)").Error)
	require.NoError(t, db.Table(table).Create(map[string]any{"id": "existing", "name": "saved", "auto_build_enabled": true}).Error)
	for range 2 {
		require.NoError(t, db.Transaction(migrateRoutePoolCreditPackFilter))
	}
	var saved marketplaceschema.RoutePool
	require.NoError(t, db.First(&saved, "id = ?", "existing").Error)
	require.Equal(t, "saved", saved.Name)
	require.True(t, saved.AutoBuildEnabled)
	require.False(t, saved.AutoBuildCreditPackOnly)
	require.NoError(t, db.Model(&saved).UpdateColumn("auto_build_credit_pack_only", true).Error)
	require.NoError(t, db.Transaction(migrateRoutePoolCreditPackFilter))
	require.NoError(t, db.First(&saved, "id = ?", "existing").Error)
	require.True(t, saved.AutoBuildCreditPackOnly)
	require.Contains(t, V2MigrationIDs(), "20261015_route_pool_credit_pack_filter")
}
