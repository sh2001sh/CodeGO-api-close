package app

import (
	"time"

	"github.com/gin-gonic/gin"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
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
	now time.Time,
) (healthy, probes, lastResort []scoredRoutePoolCandidate) {
	healthy = make([]scoredRoutePoolCandidate, 0, len(candidates))
	probes = make([]scoredRoutePoolCandidate, 0, len(candidates))
	lastResort = make([]scoredRoutePoolCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		scored, class := classifyRoutePoolCandidate(c, candidate, modelName, requestType, now)
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

func classifyRoutePoolCandidate(
	c *gin.Context,
	candidate gatewaystore.RoutePoolCandidate,
	modelName string,
	requestType gatewayruntime.RequestType,
	now time.Time,
) (scoredRoutePoolCandidate, routePoolCandidateClass) {
	if channelExcludedByScope(c, candidate.Channel) {
		gatewayruntime.ExcludeRouteDecisionCandidate(c, "non_official_channel")
		return scoredRoutePoolCandidate{}, routePoolCandidateSkip
	}
	if channelAlreadyUsed(c, candidate.Channel.Id) {
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

	health, found := gatewayruntime.GetChannelHealth(candidate.Channel.Id, modelName, requestType)
	domainHealth, domainFound := gatewayruntime.GetFaultDomainHealth(faultDomain, modelName, requestType)
	channelCooling := activeRoutePoolCircuit(health, found, now)
	domainCooling := activeRoutePoolCircuit(domainHealth, domainFound, now)
	scored := scoredRoutePoolCandidate{
		channel: candidate.Channel, score: effectiveRoutePoolCost(candidate.Member, modelName, health),
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

func activeRoutePoolCircuit(health gatewayruntime.ChannelHealth, found bool, now time.Time) bool {
	if !found {
		return false
	}
	return health.State == gatewayruntime.ChannelHealthCooling && health.CoolingUntil.After(now) ||
		health.State == gatewayruntime.ChannelHealthHalfOpen &&
			(health.CoolingUntil.After(now) || health.RecoveryProbeUntil.After(now))
}
