package app

import (
	"fmt"
	"sync"
	"time"

	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
)

const marketplaceRankingRefreshInterval = time.Minute

var marketplaceRankingTaskOnce sync.Once

// StartMarketplaceRankingTask refreshes shared ranking snapshots outside user requests.
func StartMarketplaceRankingTask() {
	marketplaceRankingTaskOnce.Do(func() {
		go func() {
			refreshPublicMarketplaceRankings(24, true)
			ticker := time.NewTicker(marketplaceRankingRefreshInterval)
			defer ticker.Stop()
			cycles := 0
			for range ticker.C {
				cycles++
				refreshPublicMarketplaceRankings(24, cycles%30 == 0)
				if cycles%15 == 0 {
					refreshPublicMarketplaceRankings(24*7, false)
				}
				if cycles%60 == 0 {
					refreshPublicMarketplaceRankings(24*30, false)
				}
			}
		}()
	})
}

func refreshPublicMarketplaceRankingsAsync(hours int) {
	go func() {
		if _, err := refreshPublicMarketplaceRankingSnapshots(hours); err != nil {
			platformobservability.SysError(fmt.Sprintf("refresh public marketplace rankings window=%d: %s", hours, err.Error()))
		}
	}()
}

func refreshPublicMarketplaceRankings(hours int, captureTrends bool) {
	groups, channels, snapshots, err := loadAndRefreshPublicMarketplaceRankings(hours)
	if err != nil {
		platformobservability.SysError(fmt.Sprintf("refresh public marketplace rankings window=%d: %s", hours, err.Error()))
		return
	}
	if hours == 24 {
		if _, recentErr := marketplaceRecentRequestSeries(groups, channels); recentErr != nil {
			platformobservability.SysError("warm marketplace recent request series: " + recentErr.Error())
		}
	}
	if !captureTrends || hours != 24 {
		return
	}
	activeGroups, activeChannels := activeMarketplaceGroups(groups, channels)
	if err := captureMultiplierTrendSnapshots(activeGroups, activeChannels, snapshots, time.Now().UTC()); err != nil {
		platformobservability.SysError("capture marketplace multiplier trends: " + err.Error())
	}
}

func refreshPublicMarketplaceRankingSnapshots(hours int) (map[string]marketplaceschema.RankingSnapshot, error) {
	_, _, snapshots, err := loadAndRefreshPublicMarketplaceRankings(hours)
	return snapshots, err
}

func loadAndRefreshPublicMarketplaceRankings(hours int) ([]marketplaceschema.Group, map[string]marketplaceschema.Channel, map[string]marketplaceschema.RankingSnapshot, error) {
	groups, channels, err := loadPublicGroups(GroupQuery{})
	if err != nil {
		return nil, nil, nil, err
	}
	snapshots, err := refreshMarketplaceRankings(groups, channels, hours)
	return groups, channels, snapshots, err
}
