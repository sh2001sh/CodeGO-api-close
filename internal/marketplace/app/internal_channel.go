package app

import (
	"strings"

	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
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
	return gatewaystore.UpdateChannel(internal)
}
