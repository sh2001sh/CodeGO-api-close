package app

import (
	"math"
	"sort"
	"time"

	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
)

// reserveRoutePoolLastResortProbe permits one cooling candidate only after
// normal candidates and expired-circuit probes have both been exhausted.
func reserveRoutePoolLastResortProbe(probes []scoredRoutePoolCandidate, modelName string) *scoredRoutePoolCandidate {
	for len(probes) > 0 {
		probe := chooseBestRoutePoolLastResortProbe(probes)
		if probe == nil {
			return nil
		}
		channelReady := !probe.channelProbe || gatewayruntime.TryStartChannelLastResortProbe(probe.channel.Id, modelName)
		domainReady := !probe.domainProbe || gatewayruntime.TryStartFaultDomainLastResortProbe(probe.faultDomain, modelName)
		if channelReady && domainReady {
			return probe
		}
		probes = removeRoutePoolCandidate(probes, probe.channel.Id)
	}
	return nil
}

func reserveRoutePoolRecoveryProbe(probes []scoredRoutePoolCandidate, modelName string) *scoredRoutePoolCandidate {
	for len(probes) > 0 {
		probe := chooseLowestRoutePoolCandidate(probes)
		if probe == nil {
			return nil
		}
		channelReady := !probe.channelProbe || gatewayruntime.TryStartChannelRecoveryProbe(probe.channel.Id, modelName)
		domainReady := !probe.domainProbe || gatewayruntime.TryStartFaultDomainRecoveryProbe(probe.faultDomain, modelName)
		if channelReady && domainReady {
			return probe
		}
		probes = removeRoutePoolCandidate(probes, probe.channel.Id)
	}
	return nil
}

// chooseBestRoutePoolLastResortProbe favors demonstrated recovery history.
// Cost is a tie-breaker because this path is only used to prevent a temporary
// all-cooling condition from becoming a broad 503 burst.
func chooseBestRoutePoolLastResortProbe(candidates []scoredRoutePoolCandidate) *scoredRoutePoolCandidate {
	if len(candidates) == 0 {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		left := routePoolLastResortProbeScore(candidates[i])
		right := routePoolLastResortProbeScore(candidates[j])
		if left == right {
			return candidates[i].cost < candidates[j].cost
		}
		return left < right
	})
	return &candidates[0]
}

func routePoolLastResortProbeScore(candidate scoredRoutePoolCandidate) float64 {
	health := candidate.health
	successRate := 70.0
	switch {
	case health.Window5Requests >= 10:
		successRate = routePoolConservativeSuccessRate(health)
	case health.Window5Requests >= 3:
		successRate = health.SuccessRate5m*0.7 + 55*0.3
	case health.SuccessRate15m > 0:
		successRate = health.SuccessRate15m*0.6 + 55*0.4
	}
	freshnessPenalty := 15.0
	if !health.LastSuccessAt.IsZero() {
		freshnessPenalty = math.Min(15, time.Since(health.LastSuccessAt).Minutes()*3)
	}
	cooldownPenalty := 0.0
	if remaining := time.Until(health.CoolingUntil); remaining > 0 {
		cooldownPenalty = math.Min(10, remaining.Seconds()/1.5)
	}
	failurePenalty := math.Min(20, float64(health.ConsecutiveRetryableFailures)*5)
	return (100 - successRate) + freshnessPenalty + cooldownPenalty + failurePenalty + candidate.cost*5
}
