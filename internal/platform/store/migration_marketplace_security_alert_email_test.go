package store

import (
	"testing"

	"github.com/glebarez/sqlite"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMarketplaceSecurityAlertEmailMigrationDefaultsExistingChannelsOff(t *testing.T) {
	oldPG := platformdb.UsingPostgreSQL
	platformdb.UsingPostgreSQL = false
	t.Cleanup(func() { platformdb.UsingPostgreSQL = oldPG })
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE marketplace_channels (
		id text primary key,
		owner_user_id integer not null,
		provider_type text not null,
		base_url_ciphertext text not null,
		credential_ciphertext text not null,
		status text not null
	)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO marketplace_channels
		(id, owner_user_id, provider_type, base_url_ciphertext, credential_ciphertext, status)
		VALUES ('existing', 42, 'openai', 'base', 'key', 'active')`).Error)

	require.NoError(t, migrateMarketplaceSecurityAlertEmail(db))
	require.NoError(t, migrateMarketplaceSecurityAlertEmail(db))
	require.True(t, db.Migrator().HasColumn(&marketplaceschema.Channel{}, "SecurityAlertEmailEnabled"))
	var enabled bool
	require.NoError(t, db.Table((marketplaceschema.Channel{}).TableName()).
		Select("security_alert_email_enabled").Where("id = ?", "existing").Scan(&enabled).Error)
	require.False(t, enabled)
	require.Contains(t, V2MigrationIDs(), "20261027_marketplace_security_alert_email")
}
