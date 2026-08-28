package app

import (
	"encoding/json"
	"time"

	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
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
	var logs []auditschema.Log
	if err := platformdb.LogDB.Where("channel_id IN ? AND type IN ? AND created_at BETWEEN ? AND ?", ids, []int{auditschema.LogTypeConsume, auditschema.LogTypeError}, startTimestamp, endTimestamp).Find(&logs).Error; err != nil {
		return nil, err
	}
	type aggregate struct {
		item      MarketplaceObservabilityItem
		latencies []int64
	}
	aggregates := map[string]*aggregate{}
	for _, log := range logs {
		channel := byID[log.ChannelId]
		key := channel.ChannelID + "\x00" + log.ModelName
		agg := aggregates[key]
		if agg == nil {
			agg = &aggregate{item: MarketplaceObservabilityItem{ChannelID: channel.ChannelID, GroupID: channel.GroupID, ChannelName: channel.Name, Model: log.ModelName}}
			aggregates[key] = agg
		}
		agg.item.RequestCount++
		if log.Type == auditschema.LogTypeConsume {
			agg.item.SuccessCount++
		} else {
			agg.item.FailedCount++
		}
		var other map[string]any
		_ = json.Unmarshal([]byte(log.Other), &other)
		latency := int64FromAny(other["total_duration_ms"])
		if latency <= 0 {
			latency = int64(log.UseTime) * 1000
		}
		if latency > 0 {
			agg.latencies = append(agg.latencies, latency)
		}
		agg.item.RetryCount += int64FromAny(other["retry_count"])
		agg.item.AvgLatencyMS += float64(latency)
		agg.item.AvgTTFTMS += float64(int64FromAny(other["attempt_ttft_ms"]))
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
