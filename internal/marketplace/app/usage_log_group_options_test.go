package app

import (
	"testing"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
)

func TestBuildUsageLogGroupOptionsUsesPublicIdentityAndPreservesOrder(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.Group{}))
	require.NoError(t, db.Create(&marketplaceschema.Channel{
		ID: "41", ApprovedSourceLabel: "Codex Plus", SourceLabelStatus: marketplacedomain.SourceLabelApproved,
	}).Error)
	require.NoError(t, db.Create(&marketplaceschema.Group{
		ID: "group-41", ChannelID: "41", InternalGroupName: "Codex-Plus-1731f7", Multiplier: 0.13,
	}).Error)

	options, err := BuildUsageLogGroupOptions([]string{
		"default", "Codex-Plus-1731f7", "default", " ",
	})
	require.NoError(t, err)
	require.Equal(t, []UsageLogGroupOption{
		{Value: "default", Label: "default"},
		{
			Value: "Codex-Plus-1731f7", Label: "41-Codex Plus-0.13x",
			PublicID: "41", MarketplaceGroupID: "group-41",
		},
	}, options)
}
