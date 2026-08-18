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

	assert.Equal(t, "provider:primary", routePoolFaultDomain(gatewayschema.RoutePoolMember{FaultDomain: " Provider:Primary "}, channel))
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
	}), 90.0)
}

func TestRoutePoolHysteresisKeepsStickyChannelForSmallImprovement(t *testing.T) {
	sticky := scoredRoutePoolCandidate{score: 1}
	nearby := scoredRoutePoolCandidate{score: 0.9}
	assert.False(t, nearby.score <= sticky.score*(1-routePoolSwitchImprovement))

	clearlyBetter := scoredRoutePoolCandidate{score: 0.84}
	assert.True(t, clearlyBetter.score <= sticky.score*(1-routePoolSwitchImprovement))
}

func TestRoutePoolLatencyMigrationRequiresReliableSlowLatencyEvidence(t *testing.T) {
	slow := gatewayruntime.ChannelHealth{TTFTSamples: 20, TTFTP95Milliseconds: 15_100}
	assert.True(t, routePoolLatencyMigrationRequired(slow, 10_000))

	insufficientSamples := slow
	insufficientSamples.TTFTSamples = 19
	assert.False(t, routePoolLatencyMigrationRequired(insufficientSamples, 10_000))

	withinNormalRange := slow
	withinNormalRange.TTFTP95Milliseconds = 15_000
	assert.False(t, routePoolLatencyMigrationRequired(withinNormalRange, 10_000))
}

func TestRoutePoolLatencyMigrationRequiresDifferentFaultDomain(t *testing.T) {
	current := &scoredRoutePoolCandidate{channel: &gatewayschema.Channel{Id: 72}, faultDomain: "provider-a", cost: 0.05}
	candidate := chooseDifferentFaultDomainRoutePoolCandidate([]scoredRoutePoolCandidate{
		*current,
		{channel: &gatewayschema.Channel{Id: 73}, faultDomain: "provider-a", cost: 0.04},
		{channel: &gatewayschema.Channel{Id: 74}, faultDomain: "provider-b", cost: 0.055},
	}, current)

	assert.NotNil(t, candidate)
	assert.Equal(t, 74, candidate.channel.Id)
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

func TestRoutePoolPreferredHealthTierChoosesStabilityBeforeCost(t *testing.T) {
	candidates := routePoolPreferredHealthTier([]scoredRoutePoolCandidate{
		{channel: &gatewayschema.Channel{Id: 39}, cost: 0.08, health: gatewayruntime.ChannelHealth{State: gatewayruntime.ChannelHealthDegraded}},
		{channel: &gatewayschema.Channel{Id: 51}, cost: 0.15, health: gatewayruntime.ChannelHealth{State: gatewayruntime.ChannelHealthHealthy}},
		{channel: &gatewayschema.Channel{Id: 44}, cost: 0.12, health: gatewayruntime.ChannelHealth{State: gatewayruntime.ChannelHealthDegraded}},
	})

	assert.Len(t, candidates, 1)
	assert.Equal(t, 51, candidates[0].channel.Id)
}

func TestRoutePoolLastResortProbeRejectsConcurrentProbeWhenLeaseIsBusy(t *testing.T) {
	channelID := 9_876_543
	modelName := "gpt-last-resort-route-pool"
	for range 3 {
		gatewayruntime.RecordChannelRetryableFailureWithCooldown(channelID, modelName, time.Minute)
	}

	probe := reserveRoutePoolLastResortProbe(nil, []scoredRoutePoolCandidate{{
		channel:      &gatewayschema.Channel{Id: channelID},
		channelProbe: true,
	}}, modelName)
	assert.NotNil(t, probe)
	assert.Equal(t, channelID, probe.channel.Id)
	concurrentProbe := reserveRoutePoolLastResortProbe(nil, []scoredRoutePoolCandidate{{
		channel:      &gatewayschema.Channel{Id: channelID},
		channelProbe: true,
	}}, modelName)
	assert.Nil(t, concurrentProbe)
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

func TestRoutePoolEmergencyRetryProbeAllowsOnlyBoundedExtraSlot(t *testing.T) {
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

	assert.NotNil(t, reserveRoutePoolEmergencyRetryProbe(nil, candidate, modelName))
	assert.NotNil(t, reserveRoutePoolEmergencyRetryProbe(nil, candidate, modelName))
	assert.Nil(t, reserveRoutePoolEmergencyRetryProbe(nil, candidate, modelName))
}
