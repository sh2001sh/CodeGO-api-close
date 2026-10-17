package runtime

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestMarketplaceEgressRequiresTrustedOwnerBinding(t *testing.T) {
	require.False(t, (*RelayInfo)(nil).UsesMarketplaceEgress())
	for _, info := range []RelayInfo{
		{},
		{MarketplaceSourceType: "official", MarketplaceOwnerID: 1, MarketplaceGroupID: "g"},
		{MarketplaceSourceType: "marketplace_user", MarketplaceOwnerID: 0, MarketplaceGroupID: "g"},
		{MarketplaceSourceType: "marketplace_user", MarketplaceOwnerID: 1},
	} {
		require.False(t, info.UsesMarketplaceEgress())
	}
	info := &RelayInfo{MarketplaceSourceType: "marketplace_user", MarketplaceOwnerID: 1, MarketplaceGroupID: "g"}
	require.True(t, info.UsesMarketplaceEgress())
}
