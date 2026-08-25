package app

import (
	"strings"

	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
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
	internal.ChannelInfo.ResponsesCapabilities = decodeMarketplaceCapabilities(channel.TransportCapabilities)
	if err := gatewaystore.UpdateChannel(internal); err != nil {
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
