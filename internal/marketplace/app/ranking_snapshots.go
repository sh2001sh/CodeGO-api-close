package app

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	auditprojection "github.com/sh2001sh/new-api/internal/audit/projection"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var marketplaceRankingRefreshes singleflight.Group

var rankingRefreshTriggers struct {
	sync.Mutex
	at map[string]time.Time
}

const rankingRefreshTriggerCooldown = 30 * time.Second
const walletBillingSourcePattern = `%"billing_source"%:%"wallet"%`

func allowRankingRefreshTrigger(key string) bool {
	now := time.Now()
	rankingRefreshTriggers.Lock()
	defer rankingRefreshTriggers.Unlock()
	if rankingRefreshTriggers.at == nil {
		rankingRefreshTriggers.at = make(map[string]time.Time)
	}
	if previous, ok := rankingRefreshTriggers.at[key]; ok && now.Sub(previous) < rankingRefreshTriggerCooldown {
		return false
	}
	rankingRefreshTriggers.at[key] = now
	return true
}

func rankingSnapshotsForRequest(groups []marketplaceschema.Group, channels map[string]marketplaceschema.Channel, hours int) (map[string]marketplaceschema.RankingSnapshot, error) {
	return rankingSnapshotsForRequestMode(groups, channels, hours, true)
}

// rankingSnapshotsForList keeps the marketplace discovery endpoint bounded by
// never making a user request wait for a cold ranking rebuild. Missing rows are
// represented as empty metrics until the background refresh writes them.
func rankingSnapshotsForList(groups []marketplaceschema.Group, channels map[string]marketplaceschema.Channel, hours int) (map[string]marketplaceschema.RankingSnapshot, error) {
	return rankingSnapshotsForRequestMode(groups, channels, hours, false)
}

func rankingSnapshotsForRequestMode(groups []marketplaceschema.Group, channels map[string]marketplaceschema.Channel, hours int, synchronousMissing bool) (map[string]marketplaceschema.RankingSnapshot, error) {
	snapshots, err := loadRankingSnapshots(groups, hours)
	if err != nil {
		return nil, err
	}
	publicGroups, _, privateGroups, privateChannels := partitionRankingGroups(groups, channels)
	publicSnapshots := selectRankingSnapshots(snapshots, publicGroups)
	if len(publicSnapshots) != len(publicGroups) {
		if synchronousMissing {
			refreshed, refreshErr := refreshPublicMarketplaceRankingSnapshots(hours)
			if refreshErr != nil {
				return nil, refreshErr
			}
			mergeRankingSnapshots(snapshots, refreshed, publicGroups)
		} else {
			refreshPublicMarketplaceRankingsAsync(hours)
		}
	} else if rankingSnapshotsStale(publicSnapshots, hours, time.Now().UTC()) {
		refreshPublicMarketplaceRankingsAsync(hours)
	}

	privateSnapshots := selectRankingSnapshots(snapshots, privateGroups)
	if len(privateSnapshots) != len(privateGroups) {
		if synchronousMissing {
			refreshed, refreshErr := refreshMarketplaceRankings(privateGroups, privateChannels, hours)
			if refreshErr != nil {
				return nil, refreshErr
			}
			mergeRankingSnapshots(snapshots, refreshed, privateGroups)
		} else {
			refreshMarketplaceRankingsAsync(privateGroups, privateChannels, hours)
		}
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
		return 10 * time.Minute
	}
}

func refreshMarketplaceRankingsAsync(groups []marketplaceschema.Group, channels map[string]marketplaceschema.Channel, hours int) {
	if len(groups) == 0 {
		return
	}
	if !allowRankingRefreshTrigger(rankingRefreshKey(groups, hours)) {
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
	// Percentiles are already aggregated from persisted and active histograms.
	// Re-reading 24 hours of raw payloads here stalls unrelated gateway queries.
	consumerStats := channelConsumerStatsByChannel(channelIDs, hours)
	snapshots := scoreMarketplaceGroups(groups, channels, totals, consumerStats, hours)
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

func scoreMarketplaceGroups(groups []marketplaceschema.Group, channels map[string]marketplaceschema.Channel, totals map[int]rankingTotals, consumerStats map[int]channelConsumerStats, hours int) []marketplaceschema.RankingSnapshot {
	snapshots := make([]marketplaceschema.RankingSnapshot, 0, len(groups))
	for _, group := range groups {
		channelID := 0
		if value := channels[group.ChannelID].InternalChannelID; value != nil {
			channelID = *value
		}
		snapshots = append(snapshots, scoreGroup(group, totals[channelID], consumerStats[channelID], hours))
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
			"avg_latency_ms", "avg_tps", "cache_hit_rate", "avg_consumer_amount", "avg_consumer_amount_by_model", "request_count", "independent_consumers", "observing", "calculated_at",
		}),
	}).CreateInBatches(&snapshots, 100).Error
}

type channelConsumerStats struct {
	IndependentConsumers int64                          `gorm:"column:independent_consumers"`
	WalletRequestCount   int64                          `gorm:"column:wallet_request_count"`
	WalletConsumerAmount int64                          `gorm:"column:wallet_consumer_amount"`
	WalletTokenCount     int64                          `gorm:"column:wallet_token_count"`
	ByModel              map[string]consumerAmountStats `gorm:"-"`
}

type consumerAmountStats struct {
	TokenCount int64
	Amount     int64
}

func (stats channelConsumerStats) averageConsumerAmount() int64 {
	if stats.WalletTokenCount <= 0 || stats.WalletConsumerAmount <= 0 {
		return 0
	}
	return int64(math.Round(float64(stats.WalletConsumerAmount) * 1_000_000 / float64(stats.WalletTokenCount)))
}

func (stats channelConsumerStats) averageConsumerAmountsByModel() map[string]int64 {
	result := make(map[string]int64, len(stats.ByModel))
	for model, values := range stats.ByModel {
		if values.TokenCount <= 0 || values.Amount <= 0 {
			continue
		}
		result[model] = int64(math.Round(float64(values.Amount) * 1_000_000 / float64(values.TokenCount)))
	}
	return result
}

func channelConsumerStatsByChannel(channelIDs []int, hours int) map[int]channelConsumerStats {
	result := make(map[int]channelConsumerStats)
	if len(channelIDs) == 0 || platformdb.LogDB == nil {
		return result
	}
	type row struct {
		ChannelID            int    `gorm:"column:channel_id"`
		ModelName            string `gorm:"column:model_name"`
		IndependentConsumers int64  `gorm:"column:independent_consumers"`
		WalletRequestCount   int64  `gorm:"column:wallet_request_count"`
		WalletConsumerAmount int64  `gorm:"column:wallet_consumer_amount"`
		WalletTokenCount     int64  `gorm:"column:wallet_token_count"`
	}
	var rows []row
	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour).Unix()
	// Consume logs store the settled billing source in Other. Keep the
	// independent-consumer threshold based on all successful requests, while
	// the displayed normalized price only reflects quota actually deducted
	// from the user's wallet. Subscription quota is never presented as an
	// out-of-pocket cost, and zero-token task records cannot distort the rate.
	base := platformdb.LogDB.Model(&auditschema.Log{}).
		Where("type = ? AND created_at >= ? AND channel_id IN ?", auditschema.LogTypeConsume, cutoff, channelIDs)
	channelRows := base.Session(&gorm.Session{}).
		Select("channel_id, COUNT(DISTINCT user_id) AS independent_consumers").
		Group("channel_id")
	modelRows := base.Session(&gorm.Session{}).
		Select(`channel_id, model_name,
			COUNT(*) AS wallet_request_count,
			COALESCE(SUM(quota), 0) AS wallet_consumer_amount,
			COALESCE(SUM(prompt_tokens + completion_tokens), 0) AS wallet_token_count`).
		Where("other LIKE ? AND prompt_tokens + completion_tokens > 0", walletBillingSourcePattern).
		Group("channel_id, model_name")
	if err := platformdb.LogDB.Table("(?) AS channel_stats", channelRows).
		Select(`channel_stats.channel_id, channel_stats.independent_consumers,
			COALESCE(model_stats.model_name, '') AS model_name,
			COALESCE(model_stats.wallet_request_count, 0) AS wallet_request_count,
			COALESCE(model_stats.wallet_consumer_amount, 0) AS wallet_consumer_amount,
			COALESCE(model_stats.wallet_token_count, 0) AS wallet_token_count`).
		Joins("LEFT JOIN (?) AS model_stats ON model_stats.channel_id = channel_stats.channel_id", modelRows).
		Scan(&rows).Error; err != nil {
		platformobservability.SysError(fmt.Sprintf("aggregate marketplace wallet consumer stats: %s", err.Error()))
		return result
	}
	for _, item := range rows {
		stats := result[item.ChannelID]
		stats.IndependentConsumers = item.IndependentConsumers
		stats.WalletRequestCount += item.WalletRequestCount
		stats.WalletConsumerAmount += item.WalletConsumerAmount
		stats.WalletTokenCount += item.WalletTokenCount
		if strings.TrimSpace(item.ModelName) != "" && item.WalletTokenCount > 0 {
			if stats.ByModel == nil {
				stats.ByModel = make(map[string]consumerAmountStats)
			}
			stats.ByModel[item.ModelName] = consumerAmountStats{TokenCount: item.WalletTokenCount, Amount: item.WalletConsumerAmount}
		}
		result[item.ChannelID] = stats
	}
	return result
}
