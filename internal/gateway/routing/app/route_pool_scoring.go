package app

import (
	"encoding/json"
	"math"
	"sort"
	"time"

	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
)

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
	if requests < 20 || successes < 0 || successes > requests {
		return health.SuccessRate5m
	}
	p := float64(successes) / float64(requests)
	z := 1.96
	denominator := 1 + z*z/float64(requests)
	center := p + z*z/(2*float64(requests))
	margin := z * math.Sqrt((p*(1-p)+z*z/(4*float64(requests)))/float64(requests))
	lowerBound := (center - margin) / denominator * 100
	return health.SuccessRate5m*0.8 + lowerBound*0.2
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

func applyRoutePoolTTFTPenalty(candidates []scoredRoutePoolCandidate, modelName string) {
	if len(candidates) < 2 {
		return
	}
	values := make([]float64, 0, len(candidates))
	for _, candidate := range candidates {
		health, found := gatewayruntime.GetChannelHealth(candidate.channel.Id, modelName)
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
		health, found := gatewayruntime.GetChannelHealth(candidates[index].channel.Id, modelName)
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

func routePoolMedianTTFT(candidates []scoredRoutePoolCandidate, modelName string) float64 {
	values := make([]float64, 0, len(candidates))
	for _, candidate := range candidates {
		health, found := gatewayruntime.GetChannelHealth(candidate.channel.Id, modelName)
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

func routePoolReliabilityNeedsMigration(health gatewayruntime.ChannelHealth) bool {
	return health.Window5Requests >= 20 && health.SuccessRate5m < 95
}
