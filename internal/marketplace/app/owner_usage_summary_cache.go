package app

import (
	"fmt"
	"slices"
	"sync"
	"time"

	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"golang.org/x/sync/singleflight"
)

const (
	ownerUsageSummaryCacheTTL        = 10 * time.Second
	ownerUsageSummaryCacheMaxEntries = 256
)

type ownerUsageSummaryCacheEntry struct {
	at      time.Time
	summary OwnerUsageLogSummary
}

var ownerUsageSummaryLoads singleflight.Group
var ownerUsageSummaryCache = struct {
	sync.Mutex
	entries map[string]ownerUsageSummaryCacheEntry
}{entries: make(map[string]ownerUsageSummaryCacheEntry)}

func ownerUsageSummaryCacheKey(ownerUserID int, channelIDs []int, groupIDs []string, query OwnerUsageLogQuery) string {
	channels := slices.Clone(channelIDs)
	slices.Sort(channels)
	groups := slices.Clone(groupIDs)
	slices.Sort(groups)
	userFilterIDs := slices.Clone(query.userFilterIDs)
	slices.Sort(userFilterIDs)
	searchUserIDs := slices.Clone(query.searchUserIDs)
	slices.Sort(searchUserIDs)
	return fmt.Sprintf(
		"%p:%p:%d:%v:%v:%d:%d:%s:%s:%s:%s:%s:%s:%v:%v",
		platformdb.DB, platformdb.LogDB, ownerUserID, channels, groups,
		query.StartTimestamp, query.EndTimestamp, query.Status, query.ModelName,
		query.RequestID, query.UpstreamRequestID, query.ExternalUserID,
		query.Search, userFilterIDs, searchUserIDs,
	)
}

func cachedOwnerUsageSummary(key string) (OwnerUsageLogSummary, bool) {
	ownerUsageSummaryCache.Lock()
	defer ownerUsageSummaryCache.Unlock()
	entry, ok := ownerUsageSummaryCache.entries[key]
	if !ok || time.Since(entry.at) >= ownerUsageSummaryCacheTTL {
		if ok {
			delete(ownerUsageSummaryCache.entries, key)
		}
		return OwnerUsageLogSummary{}, false
	}
	return entry.summary, true
}

func cacheOwnerUsageSummary(key string, summary OwnerUsageLogSummary) {
	ownerUsageSummaryCache.Lock()
	defer ownerUsageSummaryCache.Unlock()
	if len(ownerUsageSummaryCache.entries) >= ownerUsageSummaryCacheMaxEntries {
		var oldestKey string
		var oldestAt time.Time
		for entryKey, entry := range ownerUsageSummaryCache.entries {
			if oldestKey == "" || entry.at.Before(oldestAt) {
				oldestKey, oldestAt = entryKey, entry.at
			}
		}
		delete(ownerUsageSummaryCache.entries, oldestKey)
	}
	ownerUsageSummaryCache.entries[key] = ownerUsageSummaryCacheEntry{at: time.Now(), summary: summary}
}
