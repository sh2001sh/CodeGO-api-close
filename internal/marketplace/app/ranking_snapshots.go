package app

import (
	"fmt"
	"sort"
	"strings"
	"time"

	auditprojection "github.com/sh2001sh/new-api/internal/audit/projection"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm/clause"
)

var marketplaceRankingRefreshes singleflight.Group

func rankingSnapshotsForRequest(groups []marketplaceschema.Group, channels map[string]marketplaceschema.Channel, hours int) (map[string]marketplaceschema.RankingSnapshot, error) {
	snapshots, err := loadRankingSnapshots(groups, hours)
	if err != nil {
		return nil, err
	}
	publicGroups, _, privateGroups, privateChannels := partitionRankingGroups(groups, channels)
	publicSnapshots := selectRankingSnapshots(snapshots, publicGroups)
	if len(publicSnapshots) != len(publicGroups) {
		refreshed, refreshErr := refreshPublicMarketplaceRankingSnapshots(hours)
		if refreshErr != nil {
			return nil, refreshErr
		}
		mergeRankingSnapshots(snapshots, refreshed, publicGroups)
	} else if rankingSnapshotsStale(publicSnapshots, hours, time.Now().UTC()) {
		refreshPublicMarketplaceRankingsAsync(hours)
	}

	privateSnapshots := selectRankingSnapshots(snapshots, privateGroups)
	if len(privateSnapshots) != len(privateGroups) {
		refreshed, refreshErr := refreshMarketplaceRankings(privateGroups, privateChannels, hours)
		if refreshErr != nil {
			return nil, refreshErr
		}
		mergeRankingSnapshots(snapshots, refreshed, privateGroups)
	} else if rankingSnapshotsStale(privateSnapshots, hours, time.Now().UTC()) {
		refreshMarketplaceRankingsAsync(privateGroups, privateChannels, hours)
	}
	return snapshots, nil
}

func partitionRankingGroups(groups []marketplaceschema.Group, channels map[string]marketplaceschema.Channel) ([]marketplaceschema.Group, map[string]marketplaceschema.Channel, []marketplaceschema.Group, map[string]marketplaceschema.Channel) {
	publicGroups := make([]marketplaceschema.Group, 0, len(groups))
	privateGroups := make([]marketplaceschema.Group, 0)
	publicChannels := make(map[string]marketplaceschema.Channel, len(channels))
	privateChannels := make(map[string]marketplaceschema.Channel)
	for _, group := range groups {
		channel := channels[group.ChannelID]
		if group.Visibility == marketplacedomain.VisibilityPublic {
			publicGroups = append(publicGroups, group)
			publicChannels[group.ChannelID] = channel
			continue
		}
		privateGroups = append(privateGroups, group)
		privateChannels[group.ChannelID] = channel
	}
	return publicGroups, publicChannels, privateGroups, privateChannels
}

func selectRankingSnapshots(snapshots map[string]marketplaceschema.RankingSnapshot, groups []marketplaceschema.Group) map[string]marketplaceschema.RankingSnapshot {
	selected := make(map[string]marketplaceschema.RankingSnapshot, len(groups))
	for _, group := range groups {
		if snapshot, ok := snapshots[group.ID]; ok {
			selected[group.ID] = snapshot
		}
	}
	return selected
}

func mergeRankingSnapshots(target, source map[string]marketplaceschema.RankingSnapshot, groups []marketplaceschema.Group) {
	for _, group := range groups {
		if snapshot, ok := source[group.ID]; ok {
			target[group.ID] = snapshot
		}
	}
}

func loadRankingSnapshots(groups []marketplaceschema.Group, hours int) (map[string]marketplaceschema.RankingSnapshot, error) {
	result := make(map[string]marketplaceschema.RankingSnapshot, len(groups))
	groupIDs := make([]string, 0, len(groups))
	for _, group := range groups {
		groupIDs = append(groupIDs, group.ID)
	}
	if len(groupIDs) == 0 {
		return result, nil
	}
	var rows []marketplaceschema.RankingSnapshot
	err := platformdb.DB.Where(
		"group_id IN ? AND window_hours = ? AND ranking_version = ?",
		groupIDs,
		hours,
		rankingVersion,
	).Find(&rows).Error
	for _, row := range rows {
		result[row.GroupID] = row
	}
	return result, err
}

func rankingSnapshotsStale(snapshots map[string]marketplaceschema.RankingSnapshot, hours int, now time.Time) bool {
	maxAge := rankingSnapshotMaxAge(hours)
	for _, snapshot := range snapshots {
		if snapshot.CalculatedAt.IsZero() || now.Sub(snapshot.CalculatedAt) > maxAge {
			return true
		}
	}
	return false
}

func rankingSnapshotMaxAge(hours int) time.Duration {
	switch {
	case hours >= 24*30:
		return 2 * time.Hour
	case hours >= 24*7:
		return 20 * time.Minute
	default:
		return 2 * time.Minute
	}
}

func refreshMarketplaceRankingsAsync(groups []marketplaceschema.Group, channels map[string]marketplaceschema.Channel, hours int) {
	if len(groups) == 0 {
		return
	}
	go func() {
		if _, err := refreshMarketplaceRankings(groups, channels, hours); err != nil {
			platformobservability.SysError(fmt.Sprintf("refresh marketplace rankings window=%d: %s", hours, err.Error()))
		}
	}()
}

func refreshMarketplaceRankings(groups []marketplaceschema.Group, channels map[string]marketplaceschema.Channel, hours int) (map[string]marketplaceschema.RankingSnapshot, error) {
	key := rankingRefreshKey(groups, hours)
	value, err, _ := marketplaceRankingRefreshes.Do(key, func() (any, error) {
		return buildRanking(groups, channels, hours)
	})
	if err != nil {
		return nil, err
	}
	return value.(map[string]marketplaceschema.RankingSnapshot), nil
}

func rankingRefreshKey(groups []marketplaceschema.Group, hours int) string {
	groupIDs := make([]string, 0, len(groups))
	for _, group := range groups {
		groupIDs = append(groupIDs, group.ID)
	}
	sort.Strings(groupIDs)
	return fmt.Sprintf("%d:%s", hours, strings.Join(groupIDs, "\x00"))
}

func buildRanking(groups []marketplaceschema.Group, channels map[string]marketplaceschema.Channel, hours int) (map[string]marketplaceschema.RankingSnapshot, error) {
	channelIDs := marketplaceInternalChannelIDs(groups, channels)
	rows, err := auditprojection.QuerySummaryByChannels(hours, channelIDs)
	if err != nil {
		return nil, err
	}
	totals := aggregateChannelRankingRows(rows)
	consumers := independentConsumerCountsByChannel(channelIDs, hours)
	snapshots := scoreMarketplaceGroups(groups, channels, totals, consumers, hours)
	if err := persistRankingSnapshots(snapshots); err != nil {
		return nil, err
	}
	result := make(map[string]marketplaceschema.RankingSnapshot, len(snapshots))
	for _, snapshot := range snapshots {
		result[snapshot.GroupID] = snapshot
	}
	return result, nil
}

func marketplaceInternalChannelIDs(groups []marketplaceschema.Group, channels map[string]marketplaceschema.Channel) []int {
	seen := make(map[int]struct{}, len(groups))
	result := make([]int, 0, len(groups))
	for _, group := range groups {
		channelID := channels[group.ChannelID].InternalChannelID
		if channelID == nil || *channelID <= 0 {
			continue
		}
		if _, exists := seen[*channelID]; exists {
			continue
		}
		seen[*channelID] = struct{}{}
		result = append(result, *channelID)
	}
	return result
}

func scoreMarketplaceGroups(groups []marketplaceschema.Group, channels map[string]marketplaceschema.Channel, totals map[int]rankingTotals, consumers map[int]int64, hours int) []marketplaceschema.RankingSnapshot {
	snapshots := make([]marketplaceschema.RankingSnapshot, 0, len(groups))
	for _, group := range groups {
		channelID := 0
		if value := channels[group.ChannelID].InternalChannelID; value != nil {
			channelID = *value
		}
		snapshots = append(snapshots, scoreGroup(group, totals[channelID], consumers[channelID], hours))
	}
	assignRanks(snapshots)
	return snapshots
}

func persistRankingSnapshots(snapshots []marketplaceschema.RankingSnapshot) error {
	if len(snapshots) == 0 {
		return nil
	}
	return platformdb.DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "group_id"}, {Name: "window_hours"}, {Name: "ranking_version"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"rank", "score", "raw_success_rate", "wilson_success_rate", "avg_ttft_ms",
			"attempt_ttft_p50_ms", "attempt_ttft_p95_ms", "e2e_ttft_p50_ms", "e2e_ttft_p95_ms", "latency_sample_count",
			"avg_latency_ms", "avg_tps", "cache_hit_rate", "request_count", "independent_consumers", "observing", "calculated_at",
		}),
	}).CreateInBatches(&snapshots, 100).Error
}

func independentConsumerCountsByChannel(channelIDs []int, hours int) map[int]int64 {
	result := make(map[int]int64)
	if len(channelIDs) == 0 || platformdb.LogDB == nil {
		return result
	}
	type row struct {
		ChannelID int `gorm:"column:channel_id"`
		Count     int64
	}
	var rows []row
	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour).Unix()
	_ = platformdb.LogDB.Model(&auditschema.Log{}).
		Select("channel_id, COUNT(DISTINCT user_id) AS count").
		Where("type = ? AND created_at >= ? AND channel_id IN ?", auditschema.LogTypeConsume, cutoff, channelIDs).
		Group("channel_id").Scan(&rows).Error
	for _, item := range rows {
		result[item.ChannelID] = item.Count
	}
	return result
}
