package app

import (
	"strings"

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
	return gatewaystore.UpdateChannel(internal)
}
