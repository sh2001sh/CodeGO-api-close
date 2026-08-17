package app

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm/clause"
)

const multiplierSnapshotInterval = 30 * time.Minute

func ListMultiplierTrends(query MultiplierTrendQuery) (*MultiplierTrendResult, error) {
	rangeHours, bucketDuration := normalizeMultiplierTrendRange(query.RangeHours)
	groups, channels, err := loadPublicGroups(GroupQuery{})
	if err != nil {
		return nil, err
	}
	groups, channels = activeMarketplaceGroups(groups, channels)
	rankings, err := buildRanking(groups, channels, 24)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if err := captureMultiplierTrendSnapshots(groups, channels, rankings, now); err != nil {
		return nil, err
	}
	start := now.Add(-time.Duration(rangeHours) * time.Hour).Truncate(bucketDuration)
	rows, err := loadMultiplierTrendSnapshots(start, now)
	if err != nil {
		return nil, err
	}
	return &MultiplierTrendResult{
		RangeHours: rangeHours, BucketSeconds: int64(bucketDuration.Seconds()),
		Models:  marketplaceTrendModels(channels),
		Sources: buildMultiplierTrendSources(rows, strings.TrimSpace(query.Model), start, now, bucketDuration),
	}, nil
}

func normalizeMultiplierTrendRange(hours int) (int, time.Duration) {
	switch hours {
	case 24 * 7:
		return hours, 4 * time.Hour
	case 24 * 30:
		return hours, 12 * time.Hour
	default:
		return 24, multiplierSnapshotInterval
	}
}

func activeMarketplaceGroups(groups []marketplaceschema.Group, channels map[string]marketplaceschema.Channel) ([]marketplaceschema.Group, map[string]marketplaceschema.Channel) {
	filteredGroups := make([]marketplaceschema.Group, 0, len(groups))
	filteredChannels := make(map[string]marketplaceschema.Channel, len(channels))
	for _, group := range groups {
		if !marketplacedomain.AcceptsTraffic(group.LifecycleStatus) || group.VerificationStatus != marketplacedomain.VerificationPassed {
			continue
		}
		channel, ok := channels[group.ChannelID]
		if !ok {
			continue
		}
		filteredGroups = append(filteredGroups, group)
		filteredChannels[channel.ID] = channel
	}
	return filteredGroups, filteredChannels
}

func captureMultiplierTrendSnapshots(groups []marketplaceschema.Group, channels map[string]marketplaceschema.Channel, rankings map[string]marketplaceschema.RankingSnapshot, now time.Time) error {
	bucket := now.Truncate(multiplierSnapshotInterval)
	for _, group := range groups {
		channel := channels[group.ChannelID]
		models, _ := json.Marshal(decodeModels(channel.DeclaredModels))
		ranking := rankings[group.ID]
		row := marketplaceschema.MultiplierTrendSnapshot{
			GroupID: group.ID, ChannelID: channel.ID, SourceLabel: publicSourceLabel(channel), Models: string(models),
			Multiplier: group.Multiplier, Reliable: multiplierSnapshotReliable(ranking),
			RequestCount: ranking.RequestCount, WilsonSuccessRate: ranking.WilsonSuccessRate,
			BucketStartedAt: bucket, CapturedAt: now,
		}
		if err := upsertMultiplierTrendSnapshot(row); err != nil {
			return err
		}
	}
	return nil
}

func multiplierSnapshotReliable(ranking marketplaceschema.RankingSnapshot) bool {
	return !ranking.Observing && ranking.RequestCount >= 20 && ranking.WilsonSuccessRate >= 90
}

func upsertMultiplierTrendSnapshot(row marketplaceschema.MultiplierTrendSnapshot) error {
	return platformdb.DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "group_id"}, {Name: "bucket_started_at"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"channel_id", "source_label", "models", "multiplier", "reliable",
			"request_count", "wilson_success_rate", "captured_at",
		}),
	}).Create(&row).Error
}

func loadMultiplierTrendSnapshots(start, end time.Time) ([]marketplaceschema.MultiplierTrendSnapshot, error) {
	baseline, err := loadMultiplierTrendBaseline(start)
	if err != nil {
		return nil, err
	}
	var rows []marketplaceschema.MultiplierTrendSnapshot
	err = platformdb.DB.Where("bucket_started_at >= ? AND bucket_started_at <= ?", start, end).
		Order("bucket_started_at asc, group_id asc").Find(&rows).Error
	rows = append(baseline, rows...)
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].BucketStartedAt.Before(rows[j].BucketStartedAt)
	})
	return rows, err
}

func loadMultiplierTrendBaseline(start time.Time) ([]marketplaceschema.MultiplierTrendSnapshot, error) {
	var recent []marketplaceschema.MultiplierTrendSnapshot
	err := platformdb.DB.Where("bucket_started_at < ?", start).
		Order("bucket_started_at desc, group_id asc").Limit(5000).Find(&recent).Error
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	baseline := make([]marketplaceschema.MultiplierTrendSnapshot, 0)
	for _, row := range recent {
		if _, exists := seen[row.GroupID]; exists {
			continue
		}
		seen[row.GroupID] = struct{}{}
		baseline = append(baseline, row)
	}
	return baseline, nil
}

func marketplaceTrendModels(channels map[string]marketplaceschema.Channel) []string {
	seen := make(map[string]struct{})
	for _, channel := range channels {
		for _, model := range decodeModels(channel.DeclaredModels) {
			seen[model] = struct{}{}
		}
	}
	models := make([]string, 0, len(seen))
	for model := range seen {
		models = append(models, model)
	}
	sort.Strings(models)
	return models
}

func buildMultiplierTrendSources(rows []marketplaceschema.MultiplierTrendSnapshot, model string, start, end time.Time, bucketDuration time.Duration) []MultiplierTrendSource {
	rows = filterMultiplierTrendRows(rows, model)
	states := make(map[string]marketplaceschema.MultiplierTrendSnapshot)
	sourcePoints := make(map[string][]MultiplierTrendPoint)
	rowIndex := 0
	for bucket := start; !bucket.After(end); bucket = bucket.Add(bucketDuration) {
		rowIndex = applyMultiplierTrendRows(rows, rowIndex, bucket.Add(bucketDuration), states)
		appendMultiplierTrendPoints(sourcePoints, states, bucket)
	}
	sources := make([]MultiplierTrendSource, 0, len(sourcePoints))
	for source, points := range sourcePoints {
		sources = append(sources, MultiplierTrendSource{Source: source, Points: points})
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Source < sources[j].Source })
	return sources
}

func filterMultiplierTrendRows(rows []marketplaceschema.MultiplierTrendSnapshot, model string) []marketplaceschema.MultiplierTrendSnapshot {
	if model == "" {
		return rows
	}
	filtered := make([]marketplaceschema.MultiplierTrendSnapshot, 0, len(rows))
	for _, row := range rows {
		if snapshotSupportsModel(row.Models, model) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func applyMultiplierTrendRows(rows []marketplaceschema.MultiplierTrendSnapshot, index int, bucketEnd time.Time, states map[string]marketplaceschema.MultiplierTrendSnapshot) int {
	for index < len(rows) && rows[index].BucketStartedAt.Before(bucketEnd) {
		states[rows[index].GroupID] = rows[index]
		index++
	}
	return index
}

func appendMultiplierTrendPoints(target map[string][]MultiplierTrendPoint, states map[string]marketplaceschema.MultiplierTrendSnapshot, bucket time.Time) {
	bySource := make(map[string][]marketplaceschema.MultiplierTrendSnapshot)
	for _, state := range states {
		bySource[state.SourceLabel] = append(bySource[state.SourceLabel], state)
	}
	for source, candidates := range bySource {
		target[source] = append(target[source], aggregateMultiplierTrendPoint(bucket, candidates))
	}
}

func snapshotSupportsModel(raw, model string) bool {
	for _, candidate := range decodeModels(raw) {
		if strings.EqualFold(candidate, model) {
			return true
		}
	}
	return false
}

func aggregateMultiplierTrendPoint(bucket time.Time, candidates []marketplaceschema.MultiplierTrendSnapshot) MultiplierTrendPoint {
	values := make([]float64, 0, len(candidates))
	var reliableMin *float64
	reliableGroupID := ""
	eligibleCount := 0
	for _, candidate := range candidates {
		values = append(values, candidate.Multiplier)
		if candidate.Reliable {
			eligibleCount++
			reliableMin, reliableGroupID = selectReliableMinimum(reliableMin, reliableGroupID, candidate)
		}
	}
	sort.Float64s(values)
	listedMin := values[0]
	median := medianMultiplier(values)
	return MultiplierTrendPoint{
		Timestamp: bucket.Unix(), ReliableMin: reliableMin, ListedMin: &listedMin, Median: &median,
		ReliableGroupID: reliableGroupID, EligibleCount: eligibleCount, TotalCount: len(candidates),
	}
}

func selectReliableMinimum(current *float64, groupID string, candidate marketplaceschema.MultiplierTrendSnapshot) (*float64, string) {
	if current != nil && candidate.Multiplier >= *current {
		return current, groupID
	}
	value := candidate.Multiplier
	return &value, candidate.GroupID
}

func medianMultiplier(values []float64) float64 {
	middle := len(values) / 2
	if len(values)%2 == 1 {
		return values[middle]
	}
	return (values[middle-1] + values[middle]) / 2
}
