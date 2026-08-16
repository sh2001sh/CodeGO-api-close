package app

import (
	"math"
	"sort"
	"time"

	auditprojection "github.com/sh2001sh/new-api/internal/audit/projection"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const rankingVersion = "marketplace-v1"

type rankingTotals struct {
	requestCount  int64
	successWeight int64
	successTotal  float64
	latencyWeight int64
	latencyTotal  float64
	ttftWeight    int64
	ttftTotal     float64
	tpsWeight     int64
	tpsTotal      float64
	cacheHitRate  float64
}

func ListMarketplaceGroups(query GroupQuery) (*GroupListResult, error) {
	query = normalizeGroupQuery(query)
	groups, channels, err := loadPublicGroups(query)
	if err != nil {
		return nil, err
	}
	snapshots, err := buildRanking(groups, channels, query.WindowHours)
	if err != nil {
		return nil, err
	}
	recentSeries, err := marketplaceRecentRequestSeries(groups, channels, 6)
	if err != nil {
		return nil, err
	}
	items := filterAndSortGroups(groups, channels, snapshots, recentSeries, query)
	total := len(items)
	ranked := 0
	for _, item := range items {
		if !item.Observing {
			ranked++
		}
	}
	items = paginateGroups(items, query.Page, query.PageSize)
	if err := attachChannelFeedback(items, channels, query.ViewerUserID); err != nil {
		return nil, err
	}
	return &GroupListResult{Items: items, Total: total, Page: query.Page, PageSize: query.PageSize, RankedCount: ranked, WindowHours: query.WindowHours}, nil
}

func GetMarketplaceGroup(slug string, windowHours, viewerUserID int) (*GroupListItem, error) {
	query := normalizeGroupQuery(GroupQuery{ViewerUserID: viewerUserID, Search: slug, WindowHours: windowHours, Page: 1, PageSize: 50})
	result, err := ListMarketplaceGroups(query)
	if err != nil {
		return nil, err
	}
	for index := range result.Items {
		if result.Items[index].PublicSlug == slug {
			return &result.Items[index], nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func loadPublicGroups(query GroupQuery) ([]marketplaceschema.Group, map[string]marketplaceschema.Channel, error) {
	dbQuery := platformdb.DB.Model(&marketplaceschema.Group{})
	if query.ViewerUserID > 0 {
		dbQuery = dbQuery.Where("visibility = ? OR owner_user_id = ?", marketplacedomain.VisibilityPublic, query.ViewerUserID)
	} else {
		dbQuery = dbQuery.Where("visibility = ?", marketplacedomain.VisibilityPublic)
	}
	if query.Status != "" {
		dbQuery = dbQuery.Where("lifecycle_status = ?", query.Status)
	}
	if query.Verification != "" {
		dbQuery = dbQuery.Where("verification_status = ?", query.Verification)
	}
	if query.MinMultiplier > 0 {
		dbQuery = dbQuery.Where("multiplier >= ?", query.MinMultiplier)
	}
	if query.MaxMultiplier > 0 {
		dbQuery = dbQuery.Where("multiplier <= ?", query.MaxMultiplier)
	}
	var groups []marketplaceschema.Group
	if err := dbQuery.Limit(1000).Find(&groups).Error; err != nil {
		return nil, nil, err
	}
	channels, err := channelMap(groups)
	return groups, channels, err
}

func channelMap(groups []marketplaceschema.Group) (map[string]marketplaceschema.Channel, error) {
	ids := make([]string, 0, len(groups))
	for _, group := range groups {
		ids = append(ids, group.ChannelID)
	}
	result := make(map[string]marketplaceschema.Channel, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	var channels []marketplaceschema.Channel
	if err := platformdb.DB.Where("id IN ?", ids).Find(&channels).Error; err != nil {
		return nil, err
	}
	for _, channel := range channels {
		result[channel.ID] = channel
	}
	return result, nil
}

func buildRanking(groups []marketplaceschema.Group, channels map[string]marketplaceschema.Channel, hours int) (map[string]marketplaceschema.RankingSnapshot, error) {
	internalChannelIDs := make([]int, 0, len(groups))
	seenChannelIDs := make(map[int]struct{}, len(groups))
	for _, group := range groups {
		channel := channels[group.ChannelID]
		if channel.InternalChannelID == nil || *channel.InternalChannelID <= 0 {
			continue
		}
		channelID := *channel.InternalChannelID
		if _, exists := seenChannelIDs[channelID]; exists {
			continue
		}
		seenChannelIDs[channelID] = struct{}{}
		internalChannelIDs = append(internalChannelIDs, channelID)
	}
	rows, err := auditprojection.QuerySummaryByChannels(hours, internalChannelIDs)
	if err != nil {
		return nil, err
	}
	totals := aggregateChannelRankingRows(rows)
	consumers := independentConsumerCountsByChannel(internalChannelIDs, hours)
	snapshots := make([]marketplaceschema.RankingSnapshot, 0, len(groups))
	for _, group := range groups {
		channel := channels[group.ChannelID]
		channelID := 0
		if channel.InternalChannelID != nil {
			channelID = *channel.InternalChannelID
		}
		snapshots = append(snapshots, scoreGroup(group, totals[channelID], consumers[channelID], hours))
	}
	assignRanks(snapshots)
	result := make(map[string]marketplaceschema.RankingSnapshot, len(snapshots))
	for index := range snapshots {
		result[snapshots[index].GroupID] = snapshots[index]
		_ = platformdb.DB.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "group_id"}, {Name: "window_hours"}, {Name: "ranking_version"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"rank", "score", "raw_success_rate", "wilson_success_rate", "avg_ttft_ms",
				"avg_latency_ms", "avg_tps", "cache_hit_rate", "request_count", "independent_consumers", "observing", "calculated_at",
			}),
		}).Create(&snapshots[index]).Error
	}
	return result, nil
}

func aggregateChannelRankingRows(rows []auditprojection.ChannelSummary) map[int]rankingTotals {
	result := make(map[int]rankingTotals, len(rows))
	for _, row := range rows {
		result[row.ChannelID] = rankingTotals{
			requestCount: row.RequestCount, successWeight: row.RequestCount,
			successTotal:  row.SuccessRate * float64(row.RequestCount),
			latencyWeight: metricWeight(float64(row.AvgLatencyMs), row.RequestCount),
			latencyTotal:  float64(row.AvgLatencyMs) * float64(row.RequestCount),
			ttftWeight:    metricWeight(float64(row.AvgTtftMs), row.RequestCount),
			ttftTotal:     float64(row.AvgTtftMs) * float64(row.RequestCount),
			tpsWeight:     metricWeight(row.AvgTps, row.RequestCount),
			tpsTotal:      row.AvgTps * float64(row.RequestCount),
			cacheHitRate:  row.CacheHitRate,
		}
	}
	return result
}

func metricWeight(value float64, requestCount int64) int64 {
	if value <= 0 {
		return 0
	}
	return requestCount
}

func scoreGroup(group marketplaceschema.Group, total rankingTotals, consumers int64, hours int) marketplaceschema.RankingSnapshot {
	successRate := weighted(total.successTotal, total.successWeight)
	successCount := int64(math.Round(successRate / 100 * float64(total.requestCount)))
	wilson := wilsonLowerBound(successCount, total.requestCount, 1.96) * 100
	requestMin, consumerMin := rankingThresholds(hours)
	observing := total.requestCount < requestMin || consumers < consumerMin || group.VerificationStatus != marketplacedomain.VerificationPassed
	score := wilson * 0.4
	score += inverseMetricScore(weighted(total.ttftTotal, total.ttftWeight), 3000) * 0.25
	score += inverseMetricScore(weighted(total.latencyTotal, total.latencyWeight), 30000) * 0.15
	score += cappedMetricScore(weighted(total.tpsTotal, total.tpsWeight), 100) * 0.1
	score += inverseMetricScore(group.Multiplier, 3) * 0.1
	return marketplaceschema.RankingSnapshot{
		GroupID: group.ID, WindowHours: hours, RankingVersion: rankingVersion,
		Score: round1(score), RawSuccessRate: round2(successRate), WilsonSuccessRate: round2(wilson),
		AvgTTFTMs:    round2(weighted(total.ttftTotal, total.ttftWeight)),
		AvgLatencyMs: round2(weighted(total.latencyTotal, total.latencyWeight)), AvgTPS: round2(weighted(total.tpsTotal, total.tpsWeight)),
		CacheHitRate: round2(total.cacheHitRate),
		RequestCount: total.requestCount, IndependentConsumers: consumers, Observing: observing, CalculatedAt: time.Now().UTC(),
	}
}

func marketplaceRecentRequestSeries(groups []marketplaceschema.Group, channels map[string]marketplaceschema.Channel, hours int) (map[int][]RecentRequestBucket, error) {
	channelIDs := make([]int, 0, len(groups))
	seen := make(map[int]struct{}, len(groups))
	for _, group := range groups {
		channel := channels[group.ChannelID]
		if channel.InternalChannelID == nil || *channel.InternalChannelID <= 0 {
			continue
		}
		channelID := *channel.InternalChannelID
		if _, ok := seen[channelID]; ok {
			continue
		}
		seen[channelID] = struct{}{}
		channelIDs = append(channelIDs, channelID)
	}
	rows, err := auditprojection.QuerySeriesByChannels(hours, channelIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[int][]RecentRequestBucket, len(rows))
	for _, row := range rows {
		points := row.Series
		if len(points) > 12 {
			points = points[len(points)-12:]
		}
		series := make([]RecentRequestBucket, 0, len(points))
		for _, point := range points {
			series = append(series, RecentRequestBucket{Ts: point.Ts, SuccessRate: round2(point.SuccessRate), RequestCount: point.RequestCount})
		}
		result[row.ChannelID] = series
	}
	return result, nil
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

func assignRanks(snapshots []marketplaceschema.RankingSnapshot) {
	sort.SliceStable(snapshots, func(i, j int) bool {
		if snapshots[i].Observing != snapshots[j].Observing {
			return !snapshots[i].Observing
		}
		if snapshots[i].Score != snapshots[j].Score {
			return snapshots[i].Score > snapshots[j].Score
		}
		return snapshots[i].GroupID < snapshots[j].GroupID
	})
	rank := 0
	for index := range snapshots {
		if snapshots[index].Observing {
			snapshots[index].Rank = 0
			continue
		}
		rank++
		snapshots[index].Rank = rank
	}
}

func wilsonLowerBound(successes, total int64, z float64) float64 {
	if total <= 0 {
		return 0
	}
	n := float64(total)
	phat := float64(successes) / n
	z2 := z * z
	return (phat + z2/(2*n) - z*math.Sqrt((phat*(1-phat)+z2/(4*n))/n)) / (1 + z2/n)
}

func rankingThresholds(hours int) (int64, int64) {
	if hours >= 24*30 {
		return 1000, 30
	}
	if hours >= 24*7 {
		return 300, 20
	}
	return 100, 10
}

func weighted(total float64, weight int64) float64 {
	if weight <= 0 {
		return 0
	}
	return total / float64(weight)
}
func inverseMetricScore(value, ceiling float64) float64 {
	if value <= 0 {
		return 0
	}
	return math.Max(0, 100-math.Min(value/ceiling*100, 100))
}
func cappedMetricScore(value, ceiling float64) float64 {
	return math.Min(math.Max(value/ceiling*100, 0), 100)
}
func round1(value float64) float64 { return math.Round(value*10) / 10 }
func round2(value float64) float64 { return math.Round(value*100) / 100 }
