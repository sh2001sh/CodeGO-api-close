package app

import (
	"encoding/json"
	"math"
	"sort"
	"time"

	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
)

type exactLatency struct {
	AttemptP50 float64
	AttemptP95 float64
	E2EP50     float64
	E2EP95     float64
	Count      int64
}

const exactLatencyMaxRawLogHours = 24

// queryExactChannelLatency calculates percentiles from the raw request audit
// values. The persisted histogram remains the fallback for old deployments
// and for channels without retained request logs.
func queryExactChannelLatency(hours int, channelIDs []int) map[int]exactLatency {
	result := make(map[int]exactLatency)
	if platformdb.LogDB == nil || len(channelIDs) == 0 || hours <= 0 || hours > exactLatencyMaxRawLogHours {
		return result
	}
	values := make(map[int]*exactLatencyValues, len(channelIDs))
	for _, channelID := range channelIDs {
		if channelID > 0 {
			values[channelID] = &exactLatencyValues{}
		}
	}
	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour).Unix()
	var rows []struct {
		ChannelID int    `gorm:"column:channel_id"`
		Other     string `gorm:"column:other"`
	}
	if err := platformdb.LogDB.Model(&auditschema.Log{}).
		Select("channel_id, other").
		Where("type IN ? AND created_at >= ? AND channel_id IN ?", []int{auditschema.LogTypeConsume, auditschema.LogTypeError}, cutoff, channelIDs).
		Find(&rows).Error; err != nil {
		return result
	}
	for _, row := range rows {
		bucket := values[row.ChannelID]
		if bucket == nil {
			continue
		}
		var payload map[string]any
		if json.Unmarshal([]byte(row.Other), &payload) != nil {
			continue
		}
		if value, ok := positiveMetric(payload["attempt_ttft_ms"]); ok {
			bucket.Attempt = append(bucket.Attempt, value)
		}
		if value, ok := positiveMetric(payload["e2e_ttft_ms"]); ok {
			bucket.E2E = append(bucket.E2E, value)
		}
	}
	for channelID, bucket := range values {
		if len(bucket.Attempt) == 0 && len(bucket.E2E) == 0 {
			continue
		}
		result[channelID] = exactLatency{
			AttemptP50: percentile(bucket.Attempt, 0.50), AttemptP95: percentile(bucket.Attempt, 0.95),
			E2EP50: percentile(bucket.E2E, 0.50), E2EP95: percentile(bucket.E2E, 0.95),
			Count: int64(len(bucket.Attempt)),
		}
	}
	return result
}

type exactLatencyValues struct {
	Attempt []float64
	E2E     []float64
}

func positiveMetric(value any) (float64, bool) {
	number, ok := value.(float64)
	return number, ok && number >= 0
}

func percentile(values []float64, ratio float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sort.Float64s(values)
	index := int(math.Ceil(float64(len(values))*ratio)) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}
