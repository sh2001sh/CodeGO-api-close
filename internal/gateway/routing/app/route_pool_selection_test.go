package app

import (
	"testing"
	"time"

	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	"github.com/stretchr/testify/assert"
)

func TestEffectiveRoutePoolCostPrefersStableChannelOverCheapUnstableChannel(t *testing.T) {
	cheapButUnstable := effectiveRoutePoolCost(gatewayschema.RoutePoolMember{CostMultiplier: 1}, "gpt-test", gatewayruntime.ChannelHealth{
		Window5Requests: 20, SuccessRate5m: 90, State: gatewayruntime.ChannelHealthDegraded, ConsecutiveRetryableFailures: 2,
	})
	stableBackup := effectiveRoutePoolCost(gatewayschema.RoutePoolMember{CostMultiplier: 1.4}, "gpt-test", gatewayruntime.ChannelHealth{
		Window5Requests: 20, SuccessRate5m: 99, State: gatewayruntime.ChannelHealthHealthy,
	})
	assert.Greater(t, cheapButUnstable, stableBackup)
}

func TestRoutePoolModelCostOverridesMemberDefault(t *testing.T) {
	member := gatewayschema.RoutePoolMember{CostMultiplier: 1.2, ModelCostOverrides: `{"gpt-test":0.8}`}
	assert.Equal(t, 0.8, routePoolModelCost(member, "gpt-test"))
	assert.Equal(t, 1.2, routePoolModelCost(member, "other"))
}

func TestRoutePoolFaultDomainUsesConfiguredValueBeforeUpstreamHost(t *testing.T) {
	channel := &gatewayschema.Channel{Type: 1}
	baseURL := "https://proxy.example/v1"
	channel.BaseURL = &baseURL

	assert.Equal(t, "aihub:63", routePoolFaultDomain(gatewayschema.RoutePoolMember{FaultDomain: " AIHub:63 "}, channel))
	assert.Equal(t, "1:proxy.example", routePoolFaultDomain(gatewayschema.RoutePoolMember{}, channel))
}

func TestRoutePoolConservativeSuccessRatePenalizesSmallFailureSamples(t *testing.T) {
	assert.Greater(t, 98.0, routePoolConservativeSuccessRate(gatewayruntime.ChannelHealth{
		Window5Requests:  20,
		Window5Successes: 19,
		SuccessRate5m:    95,
	}))
	assert.InDelta(t, 99.0, routePoolConservativeSuccessRate(gatewayruntime.ChannelHealth{
		SuccessRate5m: 99,
	}), 0.001)
	assert.Greater(t, routePoolConservativeSuccessRate(gatewayruntime.ChannelHealth{
		Window5Requests:  20,
		Window5Successes: 20,
		SuccessRate5m:    100,
	}), 95.0)
}

func TestRoutePoolHysteresisKeepsStickyChannelForSmallImprovement(t *testing.T) {
	sticky := scoredRoutePoolCandidate{score: 1}
	nearby := scoredRoutePoolCandidate{score: 0.9}
	assert.False(t, nearby.score <= sticky.score*(1-routePoolSwitchImprovement))

	clearlyBetter := scoredRoutePoolCandidate{score: 0.84}
	assert.True(t, clearlyBetter.score <= sticky.score*(1-routePoolSwitchImprovement))
}

func TestEffectiveRoutePoolCostStronglyPenalizesPoorReliability(t *testing.T) {
	poor := effectiveRoutePoolCost(gatewayschema.RoutePoolMember{CostMultiplier: 1}, "gpt-test", gatewayruntime.ChannelHealth{
		Window5Requests: 20, Window5Successes: 18, SuccessRate5m: 90,
	})
	stable := effectiveRoutePoolCost(gatewayschema.RoutePoolMember{CostMultiplier: 1.5}, "gpt-test", gatewayruntime.ChannelHealth{
		Window5Requests: 100, Window5Successes: 100, SuccessRate5m: 100,
	})
	assert.Greater(t, poor, stable)
}

func TestRoutePoolReliabilityPenaltyDifferentiatesUnstableChannels(t *testing.T) {
	assert.Greater(t, routePoolReliabilityPenalty(60), routePoolReliabilityPenalty(75))
	assert.Greater(t, routePoolReliabilityPenalty(75), routePoolReliabilityPenalty(89))
	assert.Greater(t, routePoolReliabilityPenalty(89), routePoolReliabilityPenalty(95))
}

func TestRoutePoolRecoveryProbeRateBoostsClearlyCheaperCandidate(t *testing.T) {
	rate := routePoolRecoveryProbeRate(
		[]scoredRoutePoolCandidate{{cost: 0.15, score: 0.15}},
		[]scoredRoutePoolCandidate{{cost: 0.08, score: 0.3}},
	)
	assert.Equal(t, routePoolCostRecoveryProbeRate, rate)
}

func TestRoutePoolRecoveryProbeRateKeepsBaseRateForSimilarCost(t *testing.T) {
	rate := routePoolRecoveryProbeRate(
		[]scoredRoutePoolCandidate{{cost: 0.15, score: 0.15}},
		[]scoredRoutePoolCandidate{{cost: 0.12, score: 0.3}},
	)
	assert.Equal(t, routePoolProbeRate, rate)
}

func TestRoutePoolPrimaryCostCandidatesReserveCostlyFallback(t *testing.T) {
	candidates := routePoolPrimaryCostCandidates([]scoredRoutePoolCandidate{
		{channel: &gatewayschema.Channel{Id: 39}, cost: 0.08},
		{channel: &gatewayschema.Channel{Id: 51}, cost: 0.15},
		{channel: &gatewayschema.Channel{Id: 44}, cost: 0.12},
	})

	assert.Len(t, candidates, 2)
	assert.Equal(t, 39, candidates[0].channel.Id)
	assert.Equal(t, 44, candidates[1].channel.Id)
}

func TestRoutePoolLastResortProbeReservesCoolingCandidate(t *testing.T) {
	channelID := 9_876_543
	modelName := "gpt-last-resort-route-pool"
	for range 3 {
		gatewayruntime.RecordChannelRetryableFailureWithCooldown(channelID, modelName, time.Minute)
	}

	probe := reserveRoutePoolLastResortProbe([]scoredRoutePoolCandidate{{
		channel:      &gatewayschema.Channel{Id: channelID},
		channelProbe: true,
	}}, modelName)
	assert.NotNil(t, probe)
	assert.Equal(t, channelID, probe.channel.Id)
	assert.Nil(t, reserveRoutePoolLastResortProbe([]scoredRoutePoolCandidate{{
		channel:      &gatewayschema.Channel{Id: channelID},
		channelProbe: true,
	}}, modelName))
}

func TestRoutePoolLastResortProbePrefersReliableCandidateOverLowerCost(t *testing.T) {
	probe := chooseBestRoutePoolLastResortProbe([]scoredRoutePoolCandidate{
		{
			channel: &gatewayschema.Channel{Id: 50},
			cost:    0.04,
			health: gatewayruntime.ChannelHealth{
				Window5Requests:              20,
				Window5Successes:             12,
				SuccessRate5m:                60,
				ConsecutiveRetryableFailures: 3,
				LastSuccessAt:                time.Now().Add(-10 * time.Minute),
				CoolingUntil:                 time.Now().Add(15 * time.Second),
			},
		},
		{
			channel: &gatewayschema.Channel{Id: 52},
			cost:    0.08,
			health: gatewayruntime.ChannelHealth{
				Window5Requests:  20,
				Window5Successes: 19,
				SuccessRate5m:    95,
				LastSuccessAt:    time.Now().Add(-time.Minute),
				CoolingUntil:     time.Now().Add(5 * time.Second),
			},
		},
	})

	assert.NotNil(t, probe)
	assert.Equal(t, 52, probe.channel.Id)
}

func TestRoutePoolRateLimitRetryProbeAllowsOnlyBoundedExtraSlot(t *testing.T) {
	channelID := 9_876_544
	modelName := "gpt-rate-limit-route-pool"
	for range 3 {
		gatewayruntime.RecordChannelRetryableFailureWithCooldown(channelID, modelName, time.Minute)
	}
	candidate := []scoredRoutePoolCandidate{{
		channel:      &gatewayschema.Channel{Id: channelID},
		channelProbe: true,
		health: gatewayruntime.ChannelHealth{
			SuccessRate5m: 95,
			LastSuccessAt: time.Now().Add(-time.Minute),
		},
	}}

	assert.NotNil(t, reserveRoutePoolRateLimitRetryProbe(candidate, modelName))
	assert.NotNil(t, reserveRoutePoolRateLimitRetryProbe(candidate, modelName))
	assert.Nil(t, reserveRoutePoolRateLimitRetryProbe(candidate, modelName))
}
