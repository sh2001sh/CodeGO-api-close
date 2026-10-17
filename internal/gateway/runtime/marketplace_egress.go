package runtime

import marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"

// UsesMarketplaceEgress relies on server-resolved routing metadata, never
// client headers. Administrator-owned upstreams retain their existing policy.
func (info *RelayInfo) UsesMarketplaceEgress() bool {
	return info != nil && ((info.ChannelMeta != nil && info.ChannelMeta.MarketplaceEgress) ||
		(info.MarketplaceSourceType == marketplacedomain.SourceTypeMarketplaceUser &&
			info.MarketplaceOwnerID > 0 && info.MarketplaceGroupID != ""))
}
