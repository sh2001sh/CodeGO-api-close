package app

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
)

type routePoolCandidateClass uint8

const (
	routePoolCandidateSkip routePoolCandidateClass = iota
	routePoolCandidateHealthy
	routePoolCandidateProbe
	routePoolCandidateLastResort
)

func buildRoutePoolCandidateSets(
	c *gin.Context,
	candidates []gatewaystore.RoutePoolCandidate,
	modelName string,
	requestType gatewayruntime.RequestType,
	now time.Time, pools ...gatewayschema.RoutePool,
) (healthy, probes, lastResort []scoredRoutePoolCandidate) {
	healthy = make([]scoredRoutePoolCandidate, 0, len(candidates))
	probes = make([]scoredRoutePoolCandidate, 0, len(candidates))
	lastResort = make([]scoredRoutePoolCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		var pool *gatewayschema.RoutePool
		if len(pools) > 0 {
			pool = &pools[0]
		}
		scored, class := classifyRoutePoolCandidate(c, candidate, modelName, requestType, now, pool)
		switch class {
		case routePoolCandidateHealthy:
			healthy = append(healthy, scored)
		case routePoolCandidateProbe:
			probes = append(probes, scored)
		case routePoolCandidateLastResort:
			lastResort = append(lastResort, scored)
		}
	}
	return healthy, probes, lastResort
}

func preferRemoteCompactionCandidates(
	healthy, probes, lastResort []scoredRoutePoolCandidate,
) ([]scoredRoutePoolCandidate, []scoredRoutePoolCandidate, []scoredRoutePoolCandidate) {
	if len(healthy)+len(probes)+len(lastResort) == 0 {
		return healthy, probes, lastResort
	}
	hasSupported := func(candidates []scoredRoutePoolCandidate) bool {
		for _, candidate := range candidates {
			if candidate.compactionCapabilityRank == 0 {
				return true
			}
		}
		return false
	}
	if hasSupported(healthy) || hasSupported(probes) || hasSupported(lastResort) {
		filter := func(candidates []scoredRoutePoolCandidate) []scoredRoutePoolCandidate {
			filtered := make([]scoredRoutePoolCandidate, 0, len(candidates))
			for _, candidate := range candidates {
				if candidate.compactionCapabilityRank == 0 {
					filtered = append(filtered, candidate)
				}
			}
			return filtered
		}
		return filter(healthy), filter(probes), filter(lastResort)
	}
	return healthy, probes, lastResort
}

func classifyRoutePoolCandidate(
	c *gin.Context,
	candidate gatewaystore.RoutePoolCandidate,
	modelName string,
	requestType gatewayruntime.RequestType,
	now time.Time, pool *gatewayschema.RoutePool,
) (scoredRoutePoolCandidate, routePoolCandidateClass) {
	if pool != nil && strings.TrimSpace(pool.ModelScope) != "" && !strings.EqualFold(strings.TrimSpace(pool.ModelScope), strings.TrimSpace(modelName)) {
		return scoredRoutePoolCandidate{}, routePoolCandidateSkip
	}
	if channelExcludedByScope(c, candidate.Channel) {
		gatewayruntime.ExcludeRouteDecisionCandidate(c, "non_official_channel")
		return scoredRoutePoolCandidate{}, routePoolCandidateSkip
	}
	if channelAlreadyUsed(c, candidate.Channel.Id) {
		return scoredRoutePoolCandidate{}, routePoolCandidateSkip
	}
	compactionRank := remoteCompactionCapabilityRank(c, candidate.Channel, modelName)
	if compactionRank < 0 {
		gatewayruntime.ExcludeRouteDecisionCandidate(c, "remote_compaction_unsupported")
		return scoredRoutePoolCandidate{}, routePoolCandidateSkip
	}
	faultDomain := routePoolFaultDomain(candidate.Member, candidate.Channel)
	if gatewayruntime.IsFaultDomainExcluded(c, faultDomain) {
		gatewayruntime.ExcludeRouteDecisionCandidate(c, "failed_fault_domain")
		return scoredRoutePoolCandidate{}, routePoolCandidateSkip
	}
	if gatewayruntime.IsChannelCredentialCooling(candidate.Channel.Id) {
		gatewayruntime.ExcludeRouteDecisionCandidate(c, "channel_credential_cooling")
		return scoredRoutePoolCandidate{}, routePoolCandidateSkip
	}

	health, found := routePoolChannelHealth(c, candidate.Channel.Id, modelName, requestType)
	domainHealth, domainFound := routePoolFaultDomainHealth(c, faultDomain, modelName, requestType)
	channelCooling := activeRoutePoolCircuit(health, found, now)
	domainCooling := activeRoutePoolCircuit(domainHealth, domainFound, now)
	score := effectiveRoutePoolCostForPool(candidate.Member, modelName, health, pool)
	if compactionRank > 0 {
		score += routePoolUnknownCompactionPenalty
	}
	scored := scoredRoutePoolCandidate{
		channel: candidate.Channel, score: score, compactionCapabilityRank: compactionRank,
		cost: routePoolModelCost(candidate.Member, modelName), health: health, faultDomain: faultDomain,
	}
	if channelCooling || domainCooling {
		scored.channelProbe = channelCooling
		scored.domainProbe = domainCooling
		return scored, routePoolCandidateLastResort
	}

	scored.credentialProbe = gatewayruntime.NeedsChannelCredentialRecoveryProbe(candidate.Channel.Id)
	scored.channelProbe = found && (health.State == gatewayruntime.ChannelHealthCooling || health.State == gatewayruntime.ChannelHealthHalfOpen)
	scored.domainProbe = domainFound && (domainHealth.State == gatewayruntime.ChannelHealthCooling || domainHealth.State == gatewayruntime.ChannelHealthHalfOpen)
	if scored.channelProbe || scored.credentialProbe || scored.domainProbe {
		scored.probe = true
		return scored, routePoolCandidateProbe
	}
	return scored, routePoolCandidateHealthy
}

// routePoolChannelHealth overlays user-scoped transient state on shared
// latency/reliability observations for Auto requests. Shared state remains a
// soft score input, but cannot cool every user after one user's failures.
func routePoolChannelHealth(
	c *gin.Context,
	channelID int,
	modelName string,
	requestType gatewayruntime.RequestType,
) (gatewayruntime.ChannelHealth, bool) {
	shared, sharedFound := gatewayruntime.GetChannelHealth(channelID, modelName, requestType)
	if !gatewayruntime.IsAutoRouteRequest(c) {
		return shared, sharedFound
	}
	user, userFound := gatewayruntime.GetUserChannelHealth(c, channelID, modelName, requestType)
	shared.State = ""
	shared.ConsecutiveRetryableFailures = 0
	shared.RecoveryProbeSuccesses = 0
	shared.RecoveryProbeUntil = time.Time{}
	shared.RecoveryProbeSlots = 0
	shared.CoolingUntil = time.Time{}
	shared.LastFailureAt = time.Time{}
	shared.LastFailureRequestID = ""
	if userFound {
		shared.State = user.State
		shared.ConsecutiveRetryableFailures = user.ConsecutiveRetryableFailures
		shared.RecoveryProbeSuccesses = user.RecoveryProbeSuccesses
		shared.RecoveryProbeUntil = user.RecoveryProbeUntil
		shared.RecoveryProbeSlots = user.RecoveryProbeSlots
		shared.CoolingUntil = user.CoolingUntil
		shared.LastFailureAt = user.LastFailureAt
		shared.LastFailureRequestID = user.LastFailureRequestID
	}
	return shared, sharedFound || userFound
}

func routePoolFaultDomainHealth(
	c *gin.Context,
	domain string,
	modelName string,
	requestType gatewayruntime.RequestType,
) (gatewayruntime.ChannelHealth, bool) {
	if gatewayruntime.IsAutoRouteRequest(c) {
		return gatewayruntime.GetUserFaultDomainHealth(c, domain, modelName, requestType)
	}
	return gatewayruntime.GetFaultDomainHealth(domain, modelName, requestType)
}

func activeRoutePoolCircuit(health gatewayruntime.ChannelHealth, found bool, now time.Time) bool {
	if !found {
		return false
	}
	return health.State == gatewayruntime.ChannelHealthCooling && health.CoolingUntil.After(now) ||
		health.State == gatewayruntime.ChannelHealthHalfOpen &&
			(health.CoolingUntil.After(now) || health.RecoveryProbeUntil.After(now))
}
