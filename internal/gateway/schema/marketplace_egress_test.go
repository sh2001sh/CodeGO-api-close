package schema

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestMarketplaceEgressRequiresSelectedOwnerChannelMetadata(t *testing.T) {
	require.False(t, (*Channel)(nil).RequiresMarketplaceEgress())
	for _, metadata := range []string{"", "{", `{}`, `{"source_type":"marketplace_user"}`, `{"marketplace_channel_id":"c","marketplace_group_id":"g","source_type":"official"}`} {
		require.False(t, (&Channel{ChannelScope: ChannelScopeExternal, OtherInfo: metadata}).RequiresMarketplaceEgress())
	}
	metadata := `{"marketplace_channel_id":"c","marketplace_group_id":"g","source_type":"marketplace_user"}`
	require.False(t, (&Channel{ChannelScope: ChannelScopeOfficial, OtherInfo: metadata}).RequiresMarketplaceEgress())
	require.True(t, (&Channel{ChannelScope: ChannelScopeExternal, OtherInfo: metadata}).RequiresMarketplaceEgress())
}
