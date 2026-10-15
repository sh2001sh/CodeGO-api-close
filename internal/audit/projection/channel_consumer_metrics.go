package projection

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type channelConsumerMetricRecord struct {
	ID           int    `gorm:"primaryKey"`
	ChannelID    int    `gorm:"column:channel_id;uniqueIndex:idx_channel_consumer_bucket,priority:1;index"`
	GroupName    string `gorm:"column:group_name;size:128;uniqueIndex:idx_channel_consumer_bucket,priority:2;index"`
	ModelName    string `gorm:"column:model_name;size:128;uniqueIndex:idx_channel_consumer_bucket,priority:3"`
	BucketTs     int64  `gorm:"column:bucket_ts;uniqueIndex:idx_channel_consumer_bucket,priority:4;index"`
	RequestCount int64  `gorm:"column:request_count;not null;default:0"`
	Quota        int64  `gorm:"column:quota;not null;default:0"`
	TokenCount   int64  `gorm:"column:token_count;not null;default:0"`
}

func (channelConsumerMetricRecord) TableName() string { return "channel_consumer_metrics" }

type ChannelConsumerMetricSummary struct {
	ChannelID    int
	GroupName    string
	ModelName    string
	RequestCount int64
	Quota        int64
	TokenCount   int64
}

type channelConsumerIdentityRecord struct {
	ID        int   `gorm:"primaryKey"`
	ChannelID int   `gorm:"column:channel_id;uniqueIndex:idx_channel_consumer_identity_bucket,priority:1;index"`
	UserID    int   `gorm:"column:user_id;uniqueIndex:idx_channel_consumer_identity_bucket,priority:2"`
	BucketTs  int64 `gorm:"column:bucket_ts;uniqueIndex:idx_channel_consumer_identity_bucket,priority:3;index"`
}

func (channelConsumerIdentityRecord) TableName() string { return "channel_consumer_identities" }

type channelConsumerMetricKey struct {
	channelID int
	groupName string
	modelName string
	bucketTs  int64
}

type channelConsumerMetricCounters struct {
	requestCount atomic.Int64
	quota        atomic.Int64
	tokenCount   atomic.Int64
}

var channelConsumerMetricHot sync.Map
var channelConsumerIdentityHot sync.Map

// RecordChannelConsumerMetric records wallet-funded normalized-price inputs
// without re-scanning the raw logs table during marketplace refreshes.
func RecordChannelConsumerMetric(channelID, userID int, groupName, modelName string, quota, tokenCount int64, billingSource string) {
	groupName = strings.TrimSpace(groupName)
	modelName = strings.TrimSpace(modelName)
	if channelID <= 0 {
		return
	}
	if userID > 0 {
		identityBucketTs := time.Now().UTC().Truncate(time.Hour).Unix()
		channelConsumerIdentityHot.Store(channelConsumerIdentityRecord{ChannelID: channelID, UserID: userID, BucketTs: identityBucketTs}, struct{}{})
	}
	if modelName == "" || quota <= 0 || tokenCount <= 0 || !strings.EqualFold(strings.TrimSpace(billingSource), "wallet") {
		return
	}
	key := channelConsumerMetricKey{channelID: channelID, groupName: groupName, modelName: modelName, bucketTs: bucketStart(time.Now().Unix())}
	actual, _ := channelConsumerMetricHot.LoadOrStore(key, &channelConsumerMetricCounters{})
	counters := actual.(*channelConsumerMetricCounters)
	counters.requestCount.Add(1)
	counters.quota.Add(quota)
	counters.tokenCount.Add(tokenCount)
}

func flushCompletedChannelConsumerMetrics(_ int64) {
	channelConsumerMetricHot.Range(func(rawKey, rawValue any) bool {
		key := rawKey.(channelConsumerMetricKey)
		counters := rawValue.(*channelConsumerMetricCounters)
		requests := counters.requestCount.Swap(0)
		quota := counters.quota.Swap(0)
		tokens := counters.tokenCount.Swap(0)
		if requests <= 0 {
			if key.bucketTs < bucketStart(time.Now().Add(-24*time.Hour).Unix()) {
				channelConsumerMetricHot.Delete(rawKey)
			}
			return true
		}
		record := channelConsumerMetricRecord{ChannelID: key.channelID, GroupName: key.groupName, ModelName: key.modelName, BucketTs: key.bucketTs, RequestCount: requests, Quota: quota, TokenCount: tokens}
		if err := upsertChannelConsumerMetric(&record); err != nil {
			counters.requestCount.Add(requests)
			counters.quota.Add(quota)
			counters.tokenCount.Add(tokens)
			platformobservability.SysError(fmt.Sprintf("failed to flush channel consumer metric channel=%d model=%s bucket=%d: %s", key.channelID, key.modelName, key.bucketTs, err.Error()))
			return true
		}
		if key.bucketTs < bucketStart(time.Now().Add(-24*time.Hour).Unix()) {
			channelConsumerMetricHot.Delete(rawKey)
		}
		return true
	})
	var identities []channelConsumerIdentityRecord
	channelConsumerIdentityHot.Range(func(rawKey, _ any) bool {
		identities = append(identities, rawKey.(channelConsumerIdentityRecord))
		return true
	})
	if len(identities) > 0 {
		if err := platformdb.DB.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&identities, 500).Error; err != nil {
			platformobservability.SysError("failed to flush channel consumer identities: " + err.Error())
		} else {
			for _, identity := range identities {
				channelConsumerIdentityHot.Delete(identity)
			}
		}
	}
}

func upsertChannelConsumerMetric(record *channelConsumerMetricRecord) error {
	return platformdb.DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "channel_id"}, {Name: "group_name"}, {Name: "model_name"}, {Name: "bucket_ts"}},
		DoUpdates: clause.Assignments(map[string]any{
			"request_count": gorm.Expr("channel_consumer_metrics.request_count + ?", record.RequestCount),
			"quota":         gorm.Expr("channel_consumer_metrics.quota + ?", record.Quota),
			"token_count":   gorm.Expr("channel_consumer_metrics.token_count + ?", record.TokenCount),
		}),
	}).Create(record).Error
}

func QueryGroupConsumerMetrics(hours int, groupNames []string) ([]ChannelConsumerMetricSummary, error) {
	if len(groupNames) == 0 {
		return nil, nil
	}
	if hours <= 0 {
		hours = 24
	}
	if hours > 24*30 {
		hours = 24 * 30
	}
	endTs := time.Now().Unix()
	startTs := endTs - int64(hours)*3600
	var rows []ChannelConsumerMetricSummary
	if err := platformdb.DB.Model(&channelConsumerMetricRecord{}).
		Select("group_name, model_name, SUM(request_count) AS request_count, SUM(quota) AS quota, SUM(token_count) AS token_count").
		Where("bucket_ts >= ? AND bucket_ts <= ? AND group_name IN ?", startTs, endTs, groupNames).
		Group("group_name, model_name").Scan(&rows).Error; err != nil {
		return nil, err
	}
	byKey := make(map[string]int, len(rows))
	for index := range rows {
		byKey[rows[index].GroupName+"\x00"+rows[index].ModelName] = index
	}
	allowed := make(map[string]struct{}, len(groupNames))
	for _, groupName := range groupNames {
		allowed[groupName] = struct{}{}
	}
	channelConsumerMetricHot.Range(func(rawKey, rawValue any) bool {
		key := rawKey.(channelConsumerMetricKey)
		if key.bucketTs < startTs || key.bucketTs > endTs {
			return true
		}
		if _, ok := allowed[key.groupName]; !ok {
			return true
		}
		mapKey := key.groupName + "\x00" + key.modelName
		index, ok := byKey[mapKey]
		if !ok {
			index = len(rows)
			rows = append(rows, ChannelConsumerMetricSummary{GroupName: key.groupName, ModelName: key.modelName})
			byKey[mapKey] = index
		}
		counters := rawValue.(*channelConsumerMetricCounters)
		rows[index].RequestCount += counters.requestCount.Load()
		rows[index].Quota += counters.quota.Load()
		rows[index].TokenCount += counters.tokenCount.Load()
		return true
	})
	return rows, nil
}

func QueryChannelConsumerMetrics(hours int, channelIDs []int) ([]ChannelConsumerMetricSummary, error) {
	if len(channelIDs) == 0 {
		return nil, nil
	}
	if hours <= 0 {
		hours = 24
	}
	if hours > 24*30 {
		hours = 24 * 30
	}
	endTs := time.Now().Unix()
	startTs := endTs - int64(hours)*3600
	var rows []ChannelConsumerMetricSummary
	if err := platformdb.DB.Model(&channelConsumerMetricRecord{}).
		Select("channel_id, model_name, SUM(request_count) AS request_count, SUM(quota) AS quota, SUM(token_count) AS token_count").
		Where("bucket_ts >= ? AND bucket_ts <= ? AND channel_id IN ?", startTs, endTs, channelIDs).
		Group("channel_id, model_name").Scan(&rows).Error; err != nil {
		return nil, err
	}
	byKey := make(map[string]int, len(rows))
	for index := range rows {
		byKey[fmt.Sprintf("%d\x00%s", rows[index].ChannelID, rows[index].ModelName)] = index
	}
	allowed := make(map[int]struct{}, len(channelIDs))
	for _, channelID := range channelIDs {
		allowed[channelID] = struct{}{}
	}
	channelConsumerMetricHot.Range(func(rawKey, rawValue any) bool {
		key := rawKey.(channelConsumerMetricKey)
		if key.bucketTs < startTs || key.bucketTs > endTs {
			return true
		}
		if _, ok := allowed[key.channelID]; !ok {
			return true
		}
		counters := rawValue.(*channelConsumerMetricCounters)
		mapKey := fmt.Sprintf("%d\x00%s", key.channelID, key.modelName)
		index, ok := byKey[mapKey]
		if !ok {
			index = len(rows)
			rows = append(rows, ChannelConsumerMetricSummary{ChannelID: key.channelID, ModelName: key.modelName})
			byKey[mapKey] = index
		}
		rows[index].RequestCount += counters.requestCount.Load()
		rows[index].Quota += counters.quota.Load()
		rows[index].TokenCount += counters.tokenCount.Load()
		return true
	})
	return rows, nil
}

func QueryChannelIndependentConsumers(hours int, channelIDs []int) (map[int]int64, error) {
	result := make(map[int]int64, len(channelIDs))
	if len(channelIDs) == 0 {
		return result, nil
	}
	if hours <= 0 {
		hours = 24
	}
	if hours > 24*30 {
		hours = 24 * 30
	}
	cutoffBucket := time.Now().UTC().Add(-time.Duration(hours) * time.Hour).Truncate(time.Hour).Unix()
	type row struct {
		ChannelID int
		UserID    int
	}
	var rows []row
	if err := platformdb.DB.Model(&channelConsumerIdentityRecord{}).Select("channel_id, user_id").
		Where("bucket_ts >= ? AND channel_id IN ?", cutoffBucket, channelIDs).Group("channel_id, user_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	sets := make(map[int]map[int]struct{}, len(channelIDs))
	for _, row := range rows {
		if sets[row.ChannelID] == nil {
			sets[row.ChannelID] = make(map[int]struct{})
		}
		sets[row.ChannelID][row.UserID] = struct{}{}
	}
	allowed := make(map[int]struct{}, len(channelIDs))
	for _, channelID := range channelIDs {
		allowed[channelID] = struct{}{}
	}
	channelConsumerIdentityHot.Range(func(rawKey, _ any) bool {
		identity := rawKey.(channelConsumerIdentityRecord)
		if identity.BucketTs < cutoffBucket {
			return true
		}
		if _, ok := allowed[identity.ChannelID]; !ok {
			return true
		}
		if sets[identity.ChannelID] == nil {
			sets[identity.ChannelID] = make(map[int]struct{})
		}
		sets[identity.ChannelID][identity.UserID] = struct{}{}
		return true
	})
	for channelID, users := range sets {
		result[channelID] = int64(len(users))
	}
	return result, nil
}

func deleteChannelConsumerMetricsBefore(cutoffTs int64) error {
	if cutoffTs <= 0 {
		return nil
	}
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("bucket_ts < ?", cutoffTs).Delete(&channelConsumerMetricRecord{}).Error; err != nil {
			return err
		}
		return tx.Where("bucket_ts < ?", time.Unix(cutoffTs, 0).UTC().Truncate(time.Hour).Unix()).Delete(&channelConsumerIdentityRecord{}).Error
	})
}
