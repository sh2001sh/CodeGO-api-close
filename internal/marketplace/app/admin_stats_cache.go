package app

import (
	"sync"
	"time"
)

const adminMarketplaceStatsCacheTTL = 5 * time.Second

var adminMarketplaceStatsCache struct {
	sync.Mutex
	channelEarningsAt     time.Time
	channelEarningsKey    string
	channelEarningsResult map[string]ownerChannelEarnings
	adminChannelsAt       time.Time
	adminChannelsKey      string
	adminChannelsResult   []ChannelView
	ownerIncomeAt         time.Time
	ownerIncomeKey        string
	ownerIncomeResult     *AdminOwnerIncomeResult
}

func invalidateAdminMarketplaceStatsCache() {
	adminMarketplaceStatsCache.Lock()
	adminMarketplaceStatsCache.channelEarningsAt = time.Time{}
	adminMarketplaceStatsCache.channelEarningsKey = ""
	adminMarketplaceStatsCache.channelEarningsResult = nil
	adminMarketplaceStatsCache.adminChannelsAt = time.Time{}
	adminMarketplaceStatsCache.adminChannelsKey = ""
	adminMarketplaceStatsCache.adminChannelsResult = nil
	adminMarketplaceStatsCache.ownerIncomeAt = time.Time{}
	adminMarketplaceStatsCache.ownerIncomeKey = ""
	adminMarketplaceStatsCache.ownerIncomeResult = nil
	adminMarketplaceStatsCache.Unlock()
}

func cloneChannelViews(source []ChannelView) []ChannelView {
	return append([]ChannelView(nil), source...)
}

func cloneChannelEarnings(source map[string]ownerChannelEarnings) map[string]ownerChannelEarnings {
	result := make(map[string]ownerChannelEarnings, len(source))
	for groupID, earnings := range source {
		result[groupID] = earnings
	}
	return result
}

func cloneAdminOwnerIncomeResult(source *AdminOwnerIncomeResult) *AdminOwnerIncomeResult {
	if source == nil {
		return nil
	}
	result := *source
	result.Items = append([]AdminOwnerIncomeItem(nil), source.Items...)
	return &result
}
