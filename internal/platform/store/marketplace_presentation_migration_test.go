package store

import (
	"testing"

	"github.com/glebarez/sqlite"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMarketplacePresentationMigrationPreservesLegacyIdentity(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	table := (marketplaceschema.Group{}).TableName()
	require.NoError(t, db.Exec("CREATE TABLE "+table+" (id varchar(64) primary key, channel_id varchar(64), system_display_name varchar(128))").Error)
	require.NoError(t, db.Table(table).Create(map[string]any{"id": "group-42", "channel_id": "42", "system_display_name": "original name"}).Error)
	for range 2 {
		require.NoError(t, migrateMarketplacePresentation(db))
	}
	var group marketplaceschema.Group
	require.NoError(t, db.Unscoped().First(&group, "id = ?", "group-42").Error)
	require.Equal(t, "42", group.ChannelID)
	require.Equal(t, "original name", group.SystemDisplayName)
	for _, field := range []string{"CustomName", "Description", "Tags", "ModerationStatus", "ModerationReason", "ModerationCheckedAt"} {
		require.True(t, db.Migrator().HasColumn(&group, field), field)
	}
	require.True(t, db.Migrator().HasTable(&marketplaceschema.Shop{}))
	require.True(t, db.Migrator().HasTable(&marketplaceschema.ShopRating{}))
	require.True(t, db.Migrator().HasTable(&marketplaceschema.MarketReport{}))
}
