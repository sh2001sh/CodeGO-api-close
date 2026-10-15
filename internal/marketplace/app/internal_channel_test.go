package app

import (
	"testing"

	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
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

func TestSyncInternalChannelRefreshesRuntimeCredentialCache(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&gatewayschema.Channel{}, &gatewayschema.Ability{}))

	previousCacheEnabled := platformconfig.MemoryCacheEnabled
	platformconfig.MemoryCacheEnabled = true
	t.Cleanup(func() { platformconfig.MemoryCacheEnabled = previousCacheEnabled })
	previousSecret := platformconfig.CryptoSecret
	platformconfig.CryptoSecret = "marketplace-runtime-key-test"
	t.Cleanup(func() { platformconfig.CryptoSecret = previousSecret })

	keyA, err := platformsecurity.EncryptSecret("key-a")
	require.NoError(t, err)
	keyB, err := platformsecurity.EncryptSecret("key-b")
	require.NoError(t, err)
	internal := gatewayschema.Channel{Key: keyA, Name: "marketplace", Status: 1, Group: "market_group", Models: "gpt-5"}
	require.NoError(t, db.Create(&internal).Error)
	gatewaystore.InitChannelCache()

	channel := &marketplaceschema.Channel{
		ID: "marketplace-key-refresh", InternalChannelID: &internal.Id,
		ProviderType: "openai_compatible", DeclaredModels: `["gpt-5"]`,
		BaseURLCiphertext: "encrypted-url", CredentialCiphertext: keyB,
	}
	group := &marketplaceschema.Group{ID: "marketplace-key-refresh-group", SystemDisplayName: "Marketplace Group", InternalGroupName: "market_group"}

	require.NoError(t, syncInternalChannel(channel, group))
	cached, err := gatewaystore.GetCachedChannel(internal.Id)
	require.NoError(t, err)
	require.Equal(t, "key-b", cached.GetKeys()[0])
	require.NotEqual(t, "key-a", cached.GetKeys()[0])
}

func boolPointer(value bool) *bool { return &value }
