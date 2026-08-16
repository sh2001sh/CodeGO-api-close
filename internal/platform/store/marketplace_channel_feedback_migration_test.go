package store

import (
	"testing"

	"github.com/glebarez/sqlite"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMigrateMarketplaceChannelFeedbackMergesConflictingModelVotes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	originalPostgreSQL := platformdb.UsingPostgreSQL
	platformdb.UsingPostgreSQL = false
	t.Cleanup(func() { platformdb.UsingPostgreSQL = originalPostgreSQL })

	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &legacyMarketplaceModelFeedback{}))
	require.NoError(t, db.Create(&[]legacyMarketplaceModelFeedback{
		{ChannelID: "channel-1", UserID: 10, Model: "gpt-5", Status: "passed"},
		{ChannelID: "channel-1", UserID: 10, Model: "claude", Status: "failed"},
		{ChannelID: "channel-1", UserID: 11, Model: "gpt-5", Status: "passed"},
	}).Error)

	require.NoError(t, migrateMarketplaceChannelFeedbackAndPrices(db))
	require.False(t, db.Migrator().HasTable(&legacyMarketplaceModelFeedback{}))
	require.True(t, db.Migrator().HasColumn(&marketplaceschema.Channel{}, "ModelPrices"))
	var feedback []marketplaceschema.ChannelFeedback
	require.NoError(t, db.Order("user_id").Find(&feedback).Error)
	require.Len(t, feedback, 2)
	require.Equal(t, "questionable", feedback[0].Status)
	require.Equal(t, "passed", feedback[1].Status)
}
