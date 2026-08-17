package app

import (
	"testing"

	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
)

func TestSyncInternalChannelUpdatesMarketplaceConcurrency(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&gatewayschema.Channel{}, &gatewayschema.Ability{}))

	internal := gatewayschema.Channel{Key: "encrypted", Name: "marketplace"}
	require.NoError(t, db.Create(&internal).Error)
	channel := &marketplaceschema.Channel{
		ID: "marketplace-channel", InternalChannelID: &internal.Id,
		ProviderType: "openai_compatible", DeclaredModels: `["gpt-5"]`,
		BaseURLCiphertext: "encrypted-url", CredentialCiphertext: "encrypted-key",
		MaxConcurrency: 7, UserMaxConcurrency: 2,
		SensitiveWordInterceptionEnabled: boolPointer(false),
	}
	group := &marketplaceschema.Group{
		ID: "marketplace-group", SystemDisplayName: "Marketplace Group",
		InternalGroupName: "market_group",
	}

	require.NoError(t, syncInternalChannel(channel, group))
	require.NoError(t, db.First(&internal, internal.Id).Error)
	require.Equal(t, 7, internal.MarketplaceMaxConcurrency)
	require.Equal(t, 2, internal.MarketplaceUserMaxConcurrency)
	require.Equal(t, gatewayschema.ChannelScopeExternal, internal.ChannelScope)
	require.NotNil(t, internal.SensitiveWordInterceptionEnabled)
	require.False(t, *internal.SensitiveWordInterceptionEnabled)
}

func boolPointer(value bool) *bool { return &value }
