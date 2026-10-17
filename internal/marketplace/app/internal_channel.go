package app

import (
	"strings"

	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func syncInternalChannel(channel *marketplaceschema.Channel, group *marketplaceschema.Group) error {
	if channel == nil || group == nil || channel.InternalChannelID == nil {
		return nil
	}
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		return syncInternalChannelWithDB(tx, channel, group)
	}); err != nil {
		return err
	}
	refreshInternalChannelRuntime(*channel.InternalChannelID)
	return nil
}

func syncInternalChannelWithDB(tx *gorm.DB, channel *marketplaceschema.Channel, group *marketplaceschema.Group) error {
	var internal gatewayschema.Channel
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&internal, *channel.InternalChannelID).Error; err != nil {
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
	internal.CreditPackSupported = channel.CreditPackSupported
	internal.MultiplierCardUserEnabled = channel.MultiplierCardUserEnabled
	internal.ChannelInfo.ResponsesCapabilities = decodeMarketplaceCapabilities(channel.TransportCapabilities)
	// The internal status is owned by lifecycle controls. Keep the locked
	// value so a presentation edit cannot undo a concurrent pause.
	if err := tx.Omit("status").Save(&internal).Error; err != nil {
		return err
	}
	return gatewaystore.UpdateChannelAbilities(&internal, tx)
}

func refreshInternalChannelRuntime(internalID int) {
	// UpdateChannel persists the credential but intentionally does not rebuild
	// the gateway's in-memory routing snapshot. Refresh it before the next
	// request so replacing A with B cannot keep sending A from channelsIDM.
	gatewaystore.InitChannelCache()
	gatewaystore.NotifyChannelCacheChanged()
	platformhttpx.ResetProxyClientCache()
	gatewayruntime.InvalidateChannelAffinityForChannel(internalID)
}
