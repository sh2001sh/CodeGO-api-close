package app

import (
	"math"
	"sort"
	"strings"
	"time"

	auditprojection "github.com/sh2001sh/new-api/internal/audit/projection"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

const rankingVersion = "marketplace-v2-latency"

type rankingTotals struct {
	requestCount   int64
	successWeight  int64
	successTotal   float64
	latencyWeight  int64
	latencyTotal   float64
	attemptTtftP50 float64
	attemptTtftP95 float64
	e2eTtftP50     float64
	e2eTtftP95     float64
	latencySamples int64
	tpsWeight      int64
	tpsTotal       float64
	cacheHitRate   float64
}

func ListMarketplaceGroups(query GroupQuery) (*GroupListResult, error) {
	query = normalizeGroupQuery(query)
	groups, channels, err := loadPublicGroups(query)
	if err != nil {
		return nil, err
	}
	groups, channels = filterGroupsBySource(groups, channels, query.Source)
	snapshots, err := rankingSnapshotsForRequest(groups, channels, query.WindowHours)
	if err != nil {
		return nil, err
	}
	recentSeries, err := marketplaceRecentRequestSeries(groups, channels)
	if err != nil {
		return nil, err
	}
	items := filterAndSortGroups(groups, channels, snapshots, recentSeries, query)
	highlights := marketplaceHighlights(items)
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
	return &GroupListResult{Items: items, Highlights: highlights, Total: total, Page: query.Page, PageSize: query.PageSize, RankedCount: ranked, WindowHours: query.WindowHours}, nil
}

func filterGroupsBySource(groups []marketplaceschema.Group, channels map[string]marketplaceschema.Channel, source string) ([]marketplaceschema.Group, map[string]marketplaceschema.Channel) {
	source = strings.TrimSpace(source)
	if source == "" {
		return groups, channels
	}
	filtered := make([]marketplaceschema.Group, 0, len(groups))
	filteredChannels := make(map[string]marketplaceschema.Channel, len(groups))
	for _, group := range groups {
		channel, ok := channels[group.ChannelID]
		if !ok || !strings.EqualFold(publicSourceLabel(channel), source) {
			continue
		}
		filtered = append(filtered, group)
		filteredChannels[group.ChannelID] = channel
	}
	return filtered, filteredChannels
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
	// Suspended and disabled channels are operationally unavailable and must
	// not leak into public discovery, even when a status filter is supplied.
	dbQuery = dbQuery.Where("lifecycle_status NOT IN ?", []string{
		marketplacedomain.LifecycleSuspended,
		marketplacedomain.LifecycleDisabled,
	})
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

func aggregateChannelRankingRows(rows []auditprojection.ChannelSummary) map[int]rankingTotals {
	result := make(map[int]rankingTotals, len(rows))
	for _, row := range rows {
		result[row.ChannelID] = rankingTotals{
			requestCount: row.RequestCount, successWeight: row.RequestCount,
			successTotal:   row.SuccessRate * float64(row.RequestCount),
			latencyWeight:  metricWeight(float64(row.AvgLatencyMs), row.RequestCount),
			latencyTotal:   float64(row.AvgLatencyMs) * float64(row.RequestCount),
			attemptTtftP50: float64(row.AttemptTtftP50Ms),
			attemptTtftP95: float64(row.AttemptTtftP95Ms),
			e2eTtftP50:     float64(row.E2eTtftP50Ms),
			e2eTtftP95:     float64(row.E2eTtftP95Ms),
			latencySamples: min(row.AttemptTtftCount, row.E2eTtftCount),
			tpsWeight:      metricWeight(row.AvgTps, row.RequestCount),
			tpsTotal:       row.AvgTps * float64(row.RequestCount),
			cacheHitRate:   row.CacheHitRate,
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
	score := wilson * 0.35
	score += inverseMetricScore(total.attemptTtftP50, 3000) * 0.2
	score += inverseMetricScore(weighted(total.latencyTotal, total.latencyWeight), 30000) * 0.1
	score += cappedMetricScore(weighted(total.tpsTotal, total.tpsWeight), 100) * 0.1
	score += cappedMetricScore(total.cacheHitRate, 100) * 0.05
	score += inverseMetricScore(group.Multiplier, 3) * 0.2
	return marketplaceschema.RankingSnapshot{
		GroupID: group.ID, WindowHours: hours, RankingVersion: rankingVersion,
		Score: round1(score), RawSuccessRate: round2(successRate), WilsonSuccessRate: round2(wilson),
		// AvgTTFTMs remains a compatibility alias. New clients use the explicit
		// attempt/e2e percentile fields below.
		AvgTTFTMs:          round2(total.attemptTtftP50),
		AttemptTTFTP50Ms:   round2(total.attemptTtftP50),
		AttemptTTFTP95Ms:   round2(total.attemptTtftP95),
		E2ETTFTP50Ms:       round2(total.e2eTtftP50),
		E2ETTFTP95Ms:       round2(total.e2eTtftP95),
		LatencySampleCount: total.latencySamples,
		AvgLatencyMs:       round2(weighted(total.latencyTotal, total.latencyWeight)), AvgTPS: round2(weighted(total.tpsTotal, total.tpsWeight)),
		CacheHitRate: round2(total.cacheHitRate),
		RequestCount: total.requestCount, IndependentConsumers: consumers, Observing: observing, CalculatedAt: time.Now().UTC(),
	}
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
