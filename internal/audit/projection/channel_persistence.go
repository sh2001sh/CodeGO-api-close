package projection

import (
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type channelPerfMetricRecord struct {
	ID               int   `gorm:"primaryKey"`
	ChannelID        int   `gorm:"column:channel_id;uniqueIndex:idx_perf_channel_bucket,priority:1;index"`
	BucketTs         int64 `gorm:"column:bucket_ts;uniqueIndex:idx_perf_channel_bucket,priority:2;index:idx_channel_perf_bucket_ts"`
	RequestCount     int64 `gorm:"default:0"`
	SuccessCount     int64 `gorm:"default:0"`
	TotalLatencyMs   int64 `gorm:"default:0"`
	TtftSumMs        int64 `gorm:"default:0"`
	TtftCount        int64 `gorm:"default:0"`
	OutputTokens     int64 `gorm:"default:0"`
	GenerationMs     int64 `gorm:"default:0"`
	InputTokens      int64 `gorm:"default:0"`
	CacheReadTokens  int64 `gorm:"default:0"`
	CacheWriteTokens int64 `gorm:"default:0"`
}

func (channelPerfMetricRecord) TableName() string {
	return "channel_perf_metrics"
}

type channelPerfMetricSummaryRow struct {
	ChannelID        int   `gorm:"column:channel_id"`
	RequestCount     int64 `gorm:"column:request_count"`
	SuccessCount     int64 `gorm:"column:success_count"`
	TotalLatencyMs   int64 `gorm:"column:total_latency_ms"`
	TtftSumMs        int64 `gorm:"column:ttft_sum_ms"`
	TtftCount        int64 `gorm:"column:ttft_count"`
	OutputTokens     int64 `gorm:"column:output_tokens"`
	GenerationMs     int64 `gorm:"column:generation_ms"`
	InputTokens      int64 `gorm:"column:input_tokens"`
	CacheReadTokens  int64 `gorm:"column:cache_read_tokens"`
	CacheWriteTokens int64 `gorm:"column:cache_write_tokens"`
}

func upsertChannelMetric(record *channelPerfMetricRecord) error {
	if record == nil || record.ChannelID <= 0 || record.RequestCount == 0 {
		return nil
	}
	return platformdb.DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "channel_id"}, {Name: "bucket_ts"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"request_count":      gorm.Expr("channel_perf_metrics.request_count + ?", record.RequestCount),
			"success_count":      gorm.Expr("channel_perf_metrics.success_count + ?", record.SuccessCount),
			"total_latency_ms":   gorm.Expr("channel_perf_metrics.total_latency_ms + ?", record.TotalLatencyMs),
			"ttft_sum_ms":        gorm.Expr("channel_perf_metrics.ttft_sum_ms + ?", record.TtftSumMs),
			"ttft_count":         gorm.Expr("channel_perf_metrics.ttft_count + ?", record.TtftCount),
			"output_tokens":      gorm.Expr("channel_perf_metrics.output_tokens + ?", record.OutputTokens),
			"generation_ms":      gorm.Expr("channel_perf_metrics.generation_ms + ?", record.GenerationMs),
			"input_tokens":       gorm.Expr("channel_perf_metrics.input_tokens + ?", record.InputTokens),
			"cache_read_tokens":  gorm.Expr("channel_perf_metrics.cache_read_tokens + ?", record.CacheReadTokens),
			"cache_write_tokens": gorm.Expr("channel_perf_metrics.cache_write_tokens + ?", record.CacheWriteTokens),
		}),
	}).Create(record).Error
}

func getChannelPerfMetricSummaries(startTs, endTs int64, channelIDs []int) ([]channelPerfMetricSummaryRow, error) {
	var rows []channelPerfMetricSummaryRow
	query := platformdb.DB.Model(&channelPerfMetricRecord{}).
		Select("channel_id, SUM(request_count) AS request_count, SUM(success_count) AS success_count, SUM(total_latency_ms) AS total_latency_ms, SUM(ttft_sum_ms) AS ttft_sum_ms, SUM(ttft_count) AS ttft_count, SUM(output_tokens) AS output_tokens, SUM(generation_ms) AS generation_ms, SUM(input_tokens) AS input_tokens, SUM(cache_read_tokens) AS cache_read_tokens, SUM(cache_write_tokens) AS cache_write_tokens").
		Where("bucket_ts >= ? AND bucket_ts <= ?", startTs, endTs)
	if len(channelIDs) > 0 {
		query = query.Where("channel_id IN ?", channelIDs)
	}
	err := query.Group("channel_id").Having("SUM(request_count) > 0").Scan(&rows).Error
	return rows, err
}

func getChannelPerfMetricBuckets(startTs, endTs int64, channelIDs []int) ([]channelPerfMetricRecord, error) {
	var rows []channelPerfMetricRecord
	query := platformdb.DB.Model(&channelPerfMetricRecord{}).
		Where("bucket_ts >= ? AND bucket_ts <= ?", startTs, endTs)
	if len(channelIDs) > 0 {
		query = query.Where("channel_id IN ?", channelIDs)
	}
	err := query.Order("channel_id ASC, bucket_ts ASC").Find(&rows).Error
	return rows, err
}

func deleteChannelPerfMetricsBefore(cutoffTs int64) error {
	if cutoffTs <= 0 {
		return nil
	}
	return platformdb.DB.Where("bucket_ts < ?", cutoffTs).Delete(&channelPerfMetricRecord{}).Error
}
