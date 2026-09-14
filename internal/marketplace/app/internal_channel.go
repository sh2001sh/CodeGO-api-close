package app

import (
	"strings"

	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
)

func syncInternalChannel(channel *marketplaceschema.Channel, group *marketplaceschema.Group) error {
	if channel == nil || group == nil || channel.InternalChannelID == nil {
		return nil
	}
	internal, err := gatewaystore.LoadChannelByID(*channel.InternalChannelID, true)
	if err != nil {
		return err
	}
	baseURL := channel.BaseURLCiphertext
	internal.Type = providerChannelType(channel.ProviderType)
	internal.Name = group.SystemDisplayName
	internal.Key = channel.CredentialCiphertext
	internal.BaseURL = &baseURL
	internal.Models = strings.Join(decodeModels(channel.DeclaredModels), ",")
	internal.Group = group.InternalGroupName
	internal.ChannelScope = gatewayschema.ChannelScopeExternal
	internal.MarketplaceMaxConcurrency = channel.MaxConcurrency
	internal.MarketplaceUserMaxConcurrency = channel.UserMaxConcurrency
	internal.SensitiveWordInterceptionEnabled = channel.SensitiveWordInterceptionEnabled
	internal.MultiplierCardSupported = channel.MultiplierCardSupported
	internal.MultiplierCardUserEnabled = channel.MultiplierCardUserEnabled
	internal.ChannelInfo.ResponsesCapabilities = decodeMarketplaceCapabilities(channel.TransportCapabilities)
	if err := gatewaystore.UpdateChannel(internal); err != nil {
		return err
	}
	// The generic partial update skips zero values. Marketplace limits are
	// authoritative, and zero explicitly removes a previously configured cap.
	if err := platformdb.DB.Model(internal).Updates(map[string]interface{}{
		"marketplace_max_concurrency":      channel.MaxConcurrency,
		"marketplace_user_max_concurrency": channel.UserMaxConcurrency,
	}).Error; err != nil {
		return err
	}
	// UpdateChannel persists the credential but intentionally does not rebuild
	// the gateway's in-memory routing snapshot. Refresh it before the next
	// request so replacing A with B cannot keep sending A from channelsIDM.
	gatewaystore.InitChannelCache()
	platformhttpx.ResetProxyClientCache()
	gatewayruntime.InvalidateChannelAffinityForChannel(internal.Id)
	return nil
}
