package app

import (
	"encoding/json"
	"time"

	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
)

type MarketplaceObservabilityItem struct {
	ChannelID      string  `json:"channel_id"`
	GroupID        string  `json:"group_id"`
	ChannelName    string  `json:"channel_name"`
	Model          string  `json:"model"`
	RequestCount   int64   `json:"request_count"`
	SuccessCount   int64   `json:"success_count"`
	FailedCount    int64   `json:"failed_count"`
	SuccessRate    float64 `json:"success_rate"`
	AvgLatencyMS   float64 `json:"avg_latency_ms"`
	P95LatencyMS   int64   `json:"p95_latency_ms"`
	AvgTTFTMS      float64 `json:"avg_ttft_ms"`
	RetryCount     int64   `json:"retry_count"`
	ConsumerAmount int64   `json:"consumer_amount"`
}

type MarketplaceObservabilityView struct {
	StartTimestamp int64                          `json:"start_timestamp"`
	EndTimestamp   int64                          `json:"end_timestamp"`
	Items          []MarketplaceObservabilityItem `json:"items"`
}

func ListMarketplaceObservability(ownerUserID int, startTimestamp, endTimestamp int64) (*MarketplaceObservabilityView, error) {
	if endTimestamp <= 0 {
		endTimestamp = time.Now().Unix()
	}
	if startTimestamp <= 0 {
		startTimestamp = endTimestamp - 24*60*60
	}
	channels, err := loadOwnerUsageChannels(ownerUserID, "")
	if err != nil {
		return nil, err
	}
	if len(channels) == 0 {
		return &MarketplaceObservabilityView{StartTimestamp: startTimestamp, EndTimestamp: endTimestamp, Items: []MarketplaceObservabilityItem{}}, nil
	}
	ids := make([]int, 0, len(channels))
	byID := make(map[int]ownerUsageChannel, len(channels))
	for _, channel := range channels {
		ids = append(ids, channel.InternalChannelID)
		byID[channel.InternalChannelID] = channel
	}
	type aggregate struct {
		item      MarketplaceObservabilityItem
		latencies []int64
	}
	aggregates := map[string]*aggregate{}
	add := func(channelID int, model string, success bool, latency int64, ttft int64, retryCount int64) {
		channel := byID[channelID]
		if channel.InternalChannelID <= 0 || model == "" {
			return
		}
		key := channel.ChannelID + "\x00" + model
		agg := aggregates[key]
		if agg == nil {
			agg = &aggregate{item: MarketplaceObservabilityItem{ChannelID: channel.ChannelID, GroupID: channel.GroupID, ChannelName: channel.Name, Model: model}}
			aggregates[key] = agg
		}
		agg.item.RequestCount++
		if success {
			agg.item.SuccessCount++
		} else {
			agg.item.FailedCount++
		}
		if latency > 0 {
			agg.latencies = append(agg.latencies, latency)
		}
		agg.item.RetryCount += retryCount
		agg.item.AvgLatencyMS += float64(latency)
		agg.item.AvgTTFTMS += float64(ttft)
	}

	// Channel-owner observability uses attempt-level data when available so a
	// failed channel is not hidden by a later successful retry. Historical rows
	// fall back to the legacy final-request logs until attempts are backfilled.
	var attempts []gatewayschema.RequestAttemptAudit
	attemptsLoaded := false
	if platformdb.DB != nil {
		if err := platformdb.DB.Where("channel_id IN ? AND started_at BETWEEN ? AND ?", ids, time.Unix(startTimestamp, 0), time.Unix(endTimestamp, 0)).Find(&attempts).Error; err == nil && len(attempts) > 0 {
			attemptsLoaded = true
			for _, attempt := range attempts {
				add(attempt.ChannelID, attempt.ModelName, attempt.Success, attempt.DurationMS, 0, int64(attempt.RetryIndex))
			}
		}
	}
	if !attemptsLoaded {
		var logs []auditschema.Log
		if err := platformdb.LogDB.Where("channel_id IN ? AND type IN ? AND created_at BETWEEN ? AND ?", ids, []int{auditschema.LogTypeConsume, auditschema.LogTypeError}, startTimestamp, endTimestamp).Find(&logs).Error; err != nil {
			return nil, err
		}
		for _, log := range logs {
			var other map[string]any
			_ = json.Unmarshal([]byte(log.Other), &other)
			latency := int64FromAny(other["total_duration_ms"])
			if latency <= 0 {
				latency = int64(log.UseTime) * 1000
			}
			add(log.ChannelId, log.ModelName, log.Type == auditschema.LogTypeConsume, latency, int64FromAny(other["attempt_ttft_ms"]), int64FromAny(other["retry_count"]))
		}
	}
	items := make([]MarketplaceObservabilityItem, 0, len(aggregates))
	for _, agg := range aggregates {
		if agg.item.RequestCount > 0 {
			agg.item.SuccessRate = float64(agg.item.SuccessCount) / float64(agg.item.RequestCount)
			if len(agg.latencies) > 0 {
				agg.item.AvgLatencyMS /= float64(len(agg.latencies))
				sortInt64s(agg.latencies)
				agg.item.P95LatencyMS = agg.latencies[(len(agg.latencies)*95-1)/100]
			}
			agg.item.AvgTTFTMS /= float64(agg.item.RequestCount)
		}
		items = append(items, agg.item)
	}
	return &MarketplaceObservabilityView{StartTimestamp: startTimestamp, EndTimestamp: endTimestamp, Items: items}, nil
}

func int64FromAny(value any) int64 {
	switch number := value.(type) {
	case float64:
		return int64(number)
	case int:
		return int64(number)
	case json.Number:
		n, _ := number.Int64()
		return n
	}
	return 0
}
func sortInt64s(values []int64) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
