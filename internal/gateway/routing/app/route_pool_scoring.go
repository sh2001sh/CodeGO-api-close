package app

import (
	"encoding/json"
	"math"
	"sort"
	"time"

	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
)

// applyRoutePoolConcurrencyPenalty takes one cross-instance snapshot for all
// candidates so a busy pool member loses preference before its hard admission
// limit is reached. Telemetry failure falls back to process-local counts.
func applyRoutePoolConcurrencyPenalty(candidateSets ...[]scoredRoutePoolCandidate) {
	channelIDs := make([]int, 0)
	for _, candidates := range candidateSets {
		for _, candidate := range candidates {
			if candidate.channel != nil && candidate.channel.Id > 0 {
				channelIDs = append(channelIDs, candidate.channel.Id)
			}
		}
	}
	applyRoutePoolConcurrencyPenaltySnapshot(gatewayruntime.ActiveChannelRequestsForChannels(channelIDs), candidateSets...)
}

func applyRoutePoolConcurrencyPenaltySnapshot(active map[int]int, candidateSets ...[]scoredRoutePoolCandidate) {
	unlimitedInflight := make([]int, 0)
	for _, candidates := range candidateSets {
		for _, candidate := range candidates {
			if candidate.channel != nil && candidate.channel.MarketplaceMaxConcurrency <= 0 {
				unlimitedInflight = append(unlimitedInflight, max(0, active[candidate.channel.Id]))
			}
		}
	}
	sort.Ints(unlimitedInflight)
	unlimitedMedian := 0
	if len(unlimitedInflight) > 0 {
		unlimitedMedian = unlimitedInflight[(len(unlimitedInflight)-1)/2]
	}
	for _, candidates := range candidateSets {
		for index := range candidates {
			candidate := &candidates[index]
			if candidate.channel == nil {
				continue
			}
			candidate.inflight = max(0, active[candidate.channel.Id])
			candidate.score *= routePoolConcurrencyPenalty(
				candidate.inflight,
				candidate.channel.MarketplaceMaxConcurrency,
				unlimitedMedian,
			)
		}
	}
}

func routePoolConcurrencyPenalty(inflight, limit, unlimitedMedian int) float64 {
	if inflight <= 0 {
		return 1
	}
	if limit <= 0 {
		baseline := max(1, unlimitedMedian)
		ratio := float64(inflight) / float64(baseline)
		switch {
		case ratio >= 4:
			return 1.6
		case ratio >= 2:
			return 1.25
		default:
			return 1
		}
	}
	utilization := float64(inflight) / float64(limit)
	switch {
	case utilization < 0.5:
		return 1
	case utilization < 0.75:
		return 1 + (utilization-0.5)/0.25*0.15
	case utilization < 0.9:
		return 1.15 + (utilization-0.75)/0.15*0.35
	default:
		return 2 + math.Min(1, (utilization-0.9)/0.1)
	}
}

func routePoolCandidateLoad(candidate scoredRoutePoolCandidate) float64 {
	if candidate.channel == nil {
		return math.Inf(1)
	}
	if limit := candidate.channel.MarketplaceMaxConcurrency; limit > 0 {
		return float64(candidate.inflight) / float64(limit)
	}
	return float64(candidate.inflight)
}

func effectiveRoutePoolCost(member gatewayschema.RoutePoolMember, modelName string, health gatewayruntime.ChannelHealth) float64 {
	cost := routePoolModelCost(member, modelName)
	if cost <= 0 {
		cost = 1
	}
	if health.Window5Requests < 20 {
		cost *= 1.10
	}
	if health.Window5Requests >= 5 {
		cost *= routePoolReliabilityPenalty(routePoolConservativeSuccessRate(health))
	}
	if health.State == gatewayruntime.ChannelHealthDegraded {
		cost *= 1.35
	}
	if health.ConsecutiveRetryableFailures > 0 {
		cost *= math.Pow(1.25, float64(health.ConsecutiveRetryableFailures))
	}
	return cost
}

func effectiveRoutePoolCostForPool(member gatewayschema.RoutePoolMember, modelName string, health gatewayruntime.ChannelHealth, pool *gatewayschema.RoutePool) float64 {
	base := effectiveRoutePoolCost(member, modelName, health)
	if pool == nil {
		return base
	}
	mw, tw, cw, sw := normalizeRoutePoolWeights(pool.MultiplierWeight, pool.TTFTWeight, pool.CacheWeight, pool.SuccessWeight)
	cost := routePoolModelCost(member, modelName)
	if cost <= 0 {
		cost = 1
	}
	factor := 1 + (cost-1)*float64(mw)/100
	if health.TTFTP95Milliseconds > 0 {
		factor *= 1 + math.Min(4, health.TTFTP95Milliseconds/500)*float64(tw)/100
	}
	rate := routePoolConservativeSuccessRate(health)
	if rate > 0 {
		factor *= 1 + ((100-rate)/100)*float64(sw)/100*2
	}
	if health.CacheHitRate5m > 0 {
		factor *= 1 - (health.CacheHitRate5m/100)*float64(cw)/100*0.35
	}
	return math.Max(0.01, base*factor)
}

func normalizeRoutePoolWeights(multiplier, ttft, cache, success int) (int, int, int, int) {
	values := []int{multiplier, ttft, cache, success}
	for i, value := range values {
		if value < 0 {
			value = 0
		}
		if value > 100 {
			value = 100
		}
		values[i] = value
	}
	if values[0]+values[1]+values[2]+values[3] == 0 {
		return 35, 25, 15, 25
	}
	return values[0], values[1], values[2], values[3]
}

func routePoolReliabilityPenalty(rate float64) float64 {
	switch {
	case rate >= 98:
		return 1
	case rate >= 95:
		return 1.15 + (98-rate)*0.12
	case rate >= 90:
		return 2.5 + (95-rate)*0.3
	default:
		return math.Min(15, 5+(90-rate)*0.2)
	}
}

func routePoolConservativeSuccessRate(health gatewayruntime.ChannelHealth) float64 {
	requests := health.Window5Requests
	successes := health.Window5Successes
	if requests <= 0 || successes < 0 || successes > requests {
		return health.SuccessRate5m
	}
	alpha := 19.0 + float64(successes)
	beta := 1.0 + float64(requests-successes)
	total := alpha + beta
	mean := alpha / total
	variance := alpha * beta / (total * total * (total + 1))
	lowerBound := math.Max(0, mean-1.96*math.Sqrt(variance))
	return lowerBound * 100
}

func routePoolModelCost(member gatewayschema.RoutePoolMember, modelName string) float64 {
	cost := member.CostMultiplier
	var overrides map[string]float64
	if err := json.Unmarshal([]byte(member.ModelCostOverrides), &overrides); err == nil {
		if override, ok := overrides[modelName]; ok && override > 0 {
			cost = override
		}
	}
	return cost
}

func applyRoutePoolTTFTPenalty(candidates []scoredRoutePoolCandidate, modelName string, requestTypes ...gatewayruntime.RequestType) {
	if len(candidates) < 2 {
		return
	}
	values := make([]float64, 0, len(candidates))
	for _, candidate := range candidates {
		health, found := gatewayruntime.GetChannelHealth(candidate.channel.Id, modelName, requestTypes...)
		if found && health.TTFTP95Milliseconds > 0 {
			values = append(values, health.TTFTP95Milliseconds)
		}
	}
	if len(values) == 0 {
		return
	}
	sort.Float64s(values)
	median := values[(len(values)-1)/2]
	if median <= 0 {
		return
	}
	for index := range candidates {
		health, found := gatewayruntime.GetChannelHealth(candidates[index].channel.Id, modelName, requestTypes...)
		if !found || health.TTFTP95Milliseconds <= 0 {
			continue
		}
		ratio := health.TTFTP95Milliseconds / median
		switch {
		case ratio > 2.5:
			candidates[index].score *= 2
		case ratio > 1.5:
			candidates[index].score *= 1.35
		}
	}
}

func routePoolMedianTTFT(candidates []scoredRoutePoolCandidate, modelName string, requestTypes ...gatewayruntime.RequestType) float64 {
	values := make([]float64, 0, len(candidates))
	for _, candidate := range candidates {
		health, found := gatewayruntime.GetChannelHealth(candidate.channel.Id, modelName, requestTypes...)
		if found && health.TTFTP95Milliseconds > 0 {
			values = append(values, health.TTFTP95Milliseconds)
		}
	}
	if len(values) == 0 {
		return 0
	}
	sort.Float64s(values)
	return values[(len(values)-1)/2]
}

func routePoolHardMigrationRequired(health gatewayruntime.ChannelHealth, medianTTFT float64) bool {
	if health.State == gatewayruntime.ChannelHealthCooling && health.CoolingUntil.After(time.Now()) {
		return true
	}
	if health.ConsecutiveRetryableFailures >= 2 {
		return true
	}
	if health.Window5Requests >= 10 && routePoolConservativeSuccessRate(health) < 85 {
		return true
	}
	return medianTTFT > 0 && health.TTFTP95Milliseconds > medianTTFT*2.5
}

func routePoolLatencyMigrationRequired(health gatewayruntime.ChannelHealth, medianTTFT float64) bool {
	return health.TTFTSamples >= 20 && medianTTFT > 0 && health.TTFTP95Milliseconds > medianTTFT*1.5
}

func routePoolReliabilityNeedsMigration(health gatewayruntime.ChannelHealth) bool {
	return health.Window5Requests >= 20 && health.SuccessRate5m < 95
}
