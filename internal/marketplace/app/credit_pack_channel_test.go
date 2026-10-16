package app

import (
	"testing"

	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	"github.com/stretchr/testify/require"
)

func TestCreditPackChannelCreateRequiresExplicitOptIn(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.ChannelIDSequence{}, &identityschema.User{}))
	for _, supported := range []bool{false, true} {
		channel, _, err := buildMarketplaceRecords(db, 42, CreateChannelRequest{
			ProviderType: "openai_compatible", BaseURL: "https://upstream.example/v1", APIKey: "test-key",
			SourceLabel: "Codex Plus", DeclaredModels: []string{"gpt-5"}, Multiplier: 1,
			CreditPackSupported: supported,
		})
		require.NoError(t, err)
		require.Equal(t, supported, channel.CreditPackSupported)
	}
}

func TestCreditPackChannelOwnerCanToggleWithoutReverification(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.VerificationRun{}))
	channel := marketplaceschema.Channel{
		ID: "credit-pack-owner", OwnerUserID: 42, ProviderType: "openai_compatible",
		Status: marketplacedomain.LifecycleActive, ConnectivityTestStatus: marketplacedomain.VerificationPassed,
	}
	group := autoRouteTestGroup("credit-pack-owner-group", channel.ID, channel.OwnerUserID, 1)
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&group).Error)
	for _, supported := range []bool{true, false} {
		view, err := UpdateOwnerChannel(42, channel.ID, UpdateChannelRequest{CreditPackSupported: &supported})
		require.NoError(t, err)
		require.Equal(t, supported, view.CreditPackSupported)
		require.Equal(t, marketplacedomain.LifecycleActive, view.LifecycleStatus)
		var saved marketplaceschema.Channel
		require.NoError(t, db.First(&saved, "id = ?", channel.ID).Error)
		require.Equal(t, supported, saved.CreditPackSupported)
	}
	enabled := true
	_, err := UpdateOwnerChannel(43, channel.ID, UpdateChannelRequest{CreditPackSupported: &enabled})
	require.Error(t, err)
	var saved marketplaceschema.Channel
	require.NoError(t, db.First(&saved, "id = ?", channel.ID).Error)
	require.False(t, saved.CreditPackSupported)
}

func TestCreditPackChannelSyncPersistsDisabledAndRefreshesCache(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&gatewayschema.Channel{}, &gatewayschema.Ability{}))
	previousCacheEnabled := platformconfig.MemoryCacheEnabled
	platformconfig.MemoryCacheEnabled = true
	t.Cleanup(func() { platformconfig.MemoryCacheEnabled = previousCacheEnabled })
	internal := gatewayschema.Channel{Key: "test-key", Status: 1, Group: "credit_pack_sync", Models: "gpt-5"}
	require.NoError(t, db.Create(&internal).Error)
	channel := &marketplaceschema.Channel{
		ID: "credit-pack-sync", InternalChannelID: &internal.Id,
		ProviderType: "openai_compatible", DeclaredModels: `["gpt-5"]`,
		BaseURLCiphertext: "https://upstream.example/v1", CredentialCiphertext: "test-key",
	}
	group := &marketplaceschema.Group{ID: "credit-pack-sync-group", SystemDisplayName: "credit-pack", InternalGroupName: "credit_pack_sync"}
	for _, supported := range []bool{true, false} {
		channel.CreditPackSupported = supported
		require.NoError(t, syncInternalChannel(channel, group))
		require.NoError(t, db.First(&internal, internal.Id).Error)
		require.Equal(t, supported, internal.CreditPackSupported)
		cached, err := gatewaystore.GetCachedChannel(internal.Id)
		require.NoError(t, err)
		require.Equal(t, supported, cached.CreditPackSupported)
	}
}
