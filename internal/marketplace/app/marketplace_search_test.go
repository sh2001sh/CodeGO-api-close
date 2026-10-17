package app

import (
	"testing"

	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
)

func TestGroupSearchDoesNotExpandFromShopName(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}))
	group := seedPublicShopProduct(t, db, "702", 10)
	require.NoError(t, db.Create(&marketplaceschema.Shop{OwnerUserID: 10, Name: "晨光模型店", ModerationStatus: "passed"}).Error)
	var channel marketplaceschema.Channel
	require.NoError(t, db.First(&channel, "id = ?", group.ChannelID).Error)
	for _, tc := range []struct {
		search string
		count  int
	}{
		{search: "晨光", count: 0},
		{search: "推理渠道", count: 1},
		{search: "702", count: 1},
		{search: "gpt-5", count: 1},
		{search: "701", count: 0},
		{search: "unmatched", count: 0},
	} {
		t.Run(tc.search, func(t *testing.T) {
			items, err := filterAndSortGroups([]marketplaceschema.Group{group}, map[string]marketplaceschema.Channel{channel.ID: channel}, nil, nil, GroupQuery{Search: tc.search})
			require.NoError(t, err)
			require.Len(t, items, tc.count)
		})
	}
}
