package projection

import (
	"fmt"
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
	AttemptTtftSumMs int64 `gorm:"column:attempt_ttft_sum_ms;default:0"`
	AttemptTtftCount int64 `gorm:"column:attempt_ttft_count;default:0"`
	E2eTtftSumMs     int64 `gorm:"column:e2e_ttft_sum_ms;default:0"`
	E2eTtftCount     int64 `gorm:"column:e2e_ttft_count;default:0"`
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
	AttemptTtftSumMs int64 `gorm:"column:attempt_ttft_sum_ms"`
	AttemptTtftCount int64 `gorm:"column:attempt_ttft_count"`
	E2eTtftSumMs     int64 `gorm:"column:e2e_ttft_sum_ms"`
	E2eTtftCount     int64 `gorm:"column:e2e_ttft_count"`
}

type channelLatencyHistogramRecord struct {
	ID          int    `gorm:"primaryKey"`
	ChannelID   int    `gorm:"column:channel_id;uniqueIndex:idx_channel_latency_histogram,priority:1;index"`
	BucketTs    int64  `gorm:"column:bucket_ts;uniqueIndex:idx_channel_latency_histogram,priority:2;index:idx_channel_latency_histogram_ts"`
	Kind        string `gorm:"column:kind;size:16;uniqueIndex:idx_channel_latency_histogram,priority:3"`
	BucketIndex int    `gorm:"column:bucket_index;uniqueIndex:idx_channel_latency_histogram,priority:4"`
	SampleCount int64  `gorm:"column:sample_count;default:0"`
}

func (channelLatencyHistogramRecord) TableName() string { return "channel_latency_histograms" }

func upsertChannelMetric(record *channelPerfMetricRecord, histograms ...[]int64) error {
	if record == nil || record.ChannelID <= 0 || record.RequestCount == 0 {
		return nil
	}
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "channel_id"}, {Name: "bucket_ts"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"request_count":       gorm.Expr("channel_perf_metrics.request_count + ?", record.RequestCount),
				"success_count":       gorm.Expr("channel_perf_metrics.success_count + ?", record.SuccessCount),
				"total_latency_ms":    gorm.Expr("channel_perf_metrics.total_latency_ms + ?", record.TotalLatencyMs),
				"ttft_sum_ms":         gorm.Expr("channel_perf_metrics.ttft_sum_ms + ?", record.TtftSumMs),
				"ttft_count":          gorm.Expr("channel_perf_metrics.ttft_count + ?", record.TtftCount),
				"output_tokens":       gorm.Expr("channel_perf_metrics.output_tokens + ?", record.OutputTokens),
				"generation_ms":       gorm.Expr("channel_perf_metrics.generation_ms + ?", record.GenerationMs),
				"input_tokens":        gorm.Expr("channel_perf_metrics.input_tokens + ?", record.InputTokens),
				"cache_read_tokens":   gorm.Expr("channel_perf_metrics.cache_read_tokens + ?", record.CacheReadTokens),
				"cache_write_tokens":  gorm.Expr("channel_perf_metrics.cache_write_tokens + ?", record.CacheWriteTokens),
				"attempt_ttft_sum_ms": gorm.Expr("channel_perf_metrics.attempt_ttft_sum_ms + ?", record.AttemptTtftSumMs),
				"attempt_ttft_count":  gorm.Expr("channel_perf_metrics.attempt_ttft_count + ?", record.AttemptTtftCount),
				"e2e_ttft_sum_ms":     gorm.Expr("channel_perf_metrics.e2e_ttft_sum_ms + ?", record.E2eTtftSumMs),
				"e2e_ttft_count":      gorm.Expr("channel_perf_metrics.e2e_ttft_count + ?", record.E2eTtftCount),
			}),
		}).Create(record).Error; err != nil {
			return err
		}
		kinds := []string{"attempt", "e2e"}
		for kindIndex, values := range histograms {
			if kindIndex >= len(kinds) {
				break
			}
			for bucketIndex, count := range values {
				if count <= 0 {
					continue
				}
				row := channelLatencyHistogramRecord{ChannelID: record.ChannelID, BucketTs: record.BucketTs, Kind: kinds[kindIndex], BucketIndex: bucketIndex, SampleCount: count}
				if err := tx.Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "channel_id"}, {Name: "bucket_ts"}, {Name: "kind"}, {Name: "bucket_index"}},
					DoUpdates: clause.Assignments(map[string]interface{}{"sample_count": gorm.Expr("channel_latency_histograms.sample_count + ?", count)}),
				}).Create(&row).Error; err != nil {
					return fmt.Errorf("upsert %s latency histogram: %w", kinds[kindIndex], err)
				}
			}
		}
		return nil
	})
}

func getChannelPerfMetricSummaries(startTs, endTs int64, channelIDs []int) ([]channelPerfMetricSummaryRow, error) {
	var rows []channelPerfMetricSummaryRow
	query := platformdb.DB.Model(&channelPerfMetricRecord{}).
		Select("channel_id, SUM(request_count) AS request_count, SUM(success_count) AS success_count, SUM(total_latency_ms) AS total_latency_ms, SUM(ttft_sum_ms) AS ttft_sum_ms, SUM(ttft_count) AS ttft_count, SUM(output_tokens) AS output_tokens, SUM(generation_ms) AS generation_ms, SUM(input_tokens) AS input_tokens, SUM(cache_read_tokens) AS cache_read_tokens, SUM(cache_write_tokens) AS cache_write_tokens, SUM(attempt_ttft_sum_ms) AS attempt_ttft_sum_ms, SUM(attempt_ttft_count) AS attempt_ttft_count, SUM(e2e_ttft_sum_ms) AS e2e_ttft_sum_ms, SUM(e2e_ttft_count) AS e2e_ttft_count").
		Where("bucket_ts >= ? AND bucket_ts <= ?", startTs, endTs)
	if len(channelIDs) > 0 {
		query = query.Where("channel_id IN ?", channelIDs)
	}
	err := query.Group("channel_id").Having("SUM(request_count) > 0").Scan(&rows).Error
	return rows, err
}

func getChannelLatencyHistograms(startTs, endTs int64, channelIDs []int) (map[int]map[string][]int64, error) {
	type row struct {
		ChannelID   int
		Kind        string
		BucketIndex int
		SampleCount int64
	}
	var rows []row
	query := platformdb.DB.Model(&channelLatencyHistogramRecord{}).
		Select("channel_id, kind, bucket_index, SUM(sample_count) AS sample_count").
		Where("bucket_ts >= ? AND bucket_ts <= ?", startTs, endTs)
	if len(channelIDs) > 0 {
		query = query.Where("channel_id IN ?", channelIDs)
	}
	if err := query.Group("channel_id, kind, bucket_index").Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := make(map[int]map[string][]int64)
	for _, row := range rows {
		if row.BucketIndex < 0 || row.BucketIndex >= metricHistogramBuckets {
			continue
		}
		if result[row.ChannelID] == nil {
			result[row.ChannelID] = make(map[string][]int64)
		}
		if result[row.ChannelID][row.Kind] == nil {
			result[row.ChannelID][row.Kind] = make([]int64, metricHistogramBuckets)
		}
		result[row.ChannelID][row.Kind][row.BucketIndex] += row.SampleCount
	}
	return result, nil
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
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("bucket_ts < ?", cutoffTs).Delete(&channelLatencyHistogramRecord{}).Error; err != nil {
			return err
		}
		return tx.Where("bucket_ts < ?", cutoffTs).Delete(&channelPerfMetricRecord{}).Error
	})
}
