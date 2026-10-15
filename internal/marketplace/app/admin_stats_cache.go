package app

import (
	"golang.org/x/sync/singleflight"
	"sync"
	"time"
)

const adminMarketplaceStatsCacheTTL = 20 * time.Second
const adminMarketplaceStatsCacheMaxEntries = 256

var adminChannelsLoads singleflight.Group
var channelEarningsLoads singleflight.Group
var adminOwnerIncomeLoads singleflight.Group

type channelEarningsCacheEntry struct {
	at     time.Time
	result map[string]ownerChannelEarnings
}

var adminMarketplaceStatsCache struct {
	sync.Mutex
	channelEarnings     map[string]channelEarningsCacheEntry
	adminChannelsAt     time.Time
	adminChannelsKey    string
	adminChannelsResult []ChannelView
	ownerIncomeAt       time.Time
	ownerIncomeKey      string
	ownerIncomeResult   *AdminOwnerIncomeResult
}

func invalidateAdminMarketplaceStatsCache() {
	adminMarketplaceStatsCache.Lock()
	adminMarketplaceStatsCache.channelEarnings = nil
	adminMarketplaceStatsCache.adminChannelsAt = time.Time{}
	adminMarketplaceStatsCache.adminChannelsKey = ""
	adminMarketplaceStatsCache.adminChannelsResult = nil
	adminMarketplaceStatsCache.ownerIncomeAt = time.Time{}
	adminMarketplaceStatsCache.ownerIncomeKey = ""
	adminMarketplaceStatsCache.ownerIncomeResult = nil
	adminMarketplaceStatsCache.Unlock()
}

func cachedChannelEarnings(key string) (map[string]ownerChannelEarnings, bool) {
	adminMarketplaceStatsCache.Lock()
	defer adminMarketplaceStatsCache.Unlock()
	entry, ok := adminMarketplaceStatsCache.channelEarnings[key]
	if !ok || time.Since(entry.at) >= adminMarketplaceStatsCacheTTL {
		if ok {
			delete(adminMarketplaceStatsCache.channelEarnings, key)
		}
		return nil, false
	}
	return cloneChannelEarnings(entry.result), true
}

func cacheChannelEarnings(key string, result map[string]ownerChannelEarnings) {
	adminMarketplaceStatsCache.Lock()
	defer adminMarketplaceStatsCache.Unlock()
	if adminMarketplaceStatsCache.channelEarnings == nil {
		adminMarketplaceStatsCache.channelEarnings = make(map[string]channelEarningsCacheEntry)
	}
	if len(adminMarketplaceStatsCache.channelEarnings) >= adminMarketplaceStatsCacheMaxEntries {
		var oldestKey string
		var oldestAt time.Time
		for entryKey, entry := range adminMarketplaceStatsCache.channelEarnings {
			if oldestKey == "" || entry.at.Before(oldestAt) {
				oldestKey, oldestAt = entryKey, entry.at
			}
		}
		delete(adminMarketplaceStatsCache.channelEarnings, oldestKey)
	}
	adminMarketplaceStatsCache.channelEarnings[key] = channelEarningsCacheEntry{
		at: time.Now(), result: cloneChannelEarnings(result),
	}
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
