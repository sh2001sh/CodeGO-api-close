package app

import (
	"math"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
)

type routePoolProbeMode uint8

const (
	routePoolProbeRecovery routePoolProbeMode = iota
	routePoolProbeEmergency
	routePoolProbeLastResort
)

// reserveRoutePoolLastResortProbe permits one cooling candidate only after
// normal candidates and expired-circuit probes have both been exhausted.
func reserveRoutePoolLastResortProbe(c *gin.Context, probes []scoredRoutePoolCandidate, modelName string, requestTypes ...gatewayruntime.RequestType) *scoredRoutePoolCandidate {
	for len(probes) > 0 {
		probe := chooseBestRoutePoolLastResortProbe(probes)
		if probe == nil {
			break
		}
		channelReady := !probe.channelProbe || tryStartRoutePoolChannelProbe(c, probe.channel.Id, modelName, routePoolProbeLastResort, requestTypes...)
		credentialReady := !probe.credentialProbe || gatewayruntime.TryStartChannelCredentialRecoveryProbe(probe.channel.Id)
		domainReady := !probe.domainProbe || tryStartRoutePoolDomainProbe(c, probe.faultDomain, modelName, routePoolProbeLastResort, requestTypes...)
		if channelReady && credentialReady && domainReady {
			return probe
		}
		releasePartialRoutePoolProbe(c, probe, modelName, channelReady, credentialReady, domainReady, requestTypes...)
		probes = removeRoutePoolCandidate(probes, probe.channel.Id)
	}
	return nil
}

// reserveRoutePoolEmergencyRetryProbe is a bounded escape hatch for a retry
// after a transient upstream error. The failed fault domain has already been
// excluded from the request context, so it chooses the best remaining route
// while allowing one extra channel/domain probe slot.
func reserveRoutePoolEmergencyRetryProbe(c *gin.Context, probes []scoredRoutePoolCandidate, modelName string, requestTypes ...gatewayruntime.RequestType) *scoredRoutePoolCandidate {
	for len(probes) > 0 {
		probe := chooseBestRoutePoolLastResortProbe(probes)
		if probe == nil {
			return nil
		}
		channelReady := !probe.channelProbe || tryStartRoutePoolChannelProbe(c, probe.channel.Id, modelName, routePoolProbeEmergency, requestTypes...)
		credentialReady := !probe.credentialProbe || gatewayruntime.TryStartChannelCredentialRecoveryProbe(probe.channel.Id)
		domainReady := !probe.domainProbe || tryStartRoutePoolDomainProbe(c, probe.faultDomain, modelName, routePoolProbeEmergency, requestTypes...)
		if channelReady && credentialReady && domainReady {
			return probe
		}
		releasePartialRoutePoolProbe(c, probe, modelName, channelReady, credentialReady, domainReady, requestTypes...)
		probes = removeRoutePoolCandidate(probes, probe.channel.Id)
	}
	return nil
}

func reserveRoutePoolRecoveryProbe(c *gin.Context, probes []scoredRoutePoolCandidate, modelName string, requestTypes ...gatewayruntime.RequestType) *scoredRoutePoolCandidate {
	for len(probes) > 0 {
		probe := chooseLowestRoutePoolCandidate(probes)
		if probe == nil {
			return nil
		}
		channelReady := !probe.channelProbe || tryStartRoutePoolChannelProbe(c, probe.channel.Id, modelName, routePoolProbeRecovery, requestTypes...)
		credentialReady := !probe.credentialProbe || gatewayruntime.TryStartChannelCredentialRecoveryProbe(probe.channel.Id)
		domainReady := !probe.domainProbe || tryStartRoutePoolDomainProbe(c, probe.faultDomain, modelName, routePoolProbeRecovery, requestTypes...)
		if channelReady && credentialReady && domainReady {
			return probe
		}
		releasePartialRoutePoolProbe(c, probe, modelName, channelReady, credentialReady, domainReady, requestTypes...)
		probes = removeRoutePoolCandidate(probes, probe.channel.Id)
	}
	return nil
}

func tryStartRoutePoolChannelProbe(c *gin.Context, channelID int, modelName string, mode routePoolProbeMode, requestTypes ...gatewayruntime.RequestType) bool {
	if gatewayruntime.IsAutoRouteRequest(c) {
		switch mode {
		case routePoolProbeRecovery:
			return gatewayruntime.TryStartUserChannelRecoveryProbe(c, channelID, modelName, requestTypes...)
		case routePoolProbeEmergency:
			return gatewayruntime.TryStartUserChannelEmergencyProbe(c, channelID, modelName, requestTypes...)
		default:
			return gatewayruntime.TryStartUserChannelLastResortProbe(c, channelID, modelName, requestTypes...)
		}
	}
	switch mode {
	case routePoolProbeRecovery:
		return gatewayruntime.TryStartChannelRecoveryProbe(channelID, modelName, requestTypes...)
	case routePoolProbeEmergency:
		return gatewayruntime.TryStartChannelEmergencyRetryProbe(channelID, modelName, requestTypes...)
	default:
		return gatewayruntime.TryStartChannelLastResortProbe(channelID, modelName, requestTypes...)
	}
}

func tryStartRoutePoolDomainProbe(c *gin.Context, domain, modelName string, mode routePoolProbeMode, requestTypes ...gatewayruntime.RequestType) bool {
	if gatewayruntime.IsAutoRouteRequest(c) {
		switch mode {
		case routePoolProbeRecovery:
			return gatewayruntime.TryStartUserFaultDomainRecoveryProbe(c, domain, modelName, requestTypes...)
		case routePoolProbeEmergency:
			return gatewayruntime.TryStartUserFaultDomainEmergencyProbe(c, domain, modelName, requestTypes...)
		default:
			return gatewayruntime.TryStartUserFaultDomainLastResortProbe(c, domain, modelName, requestTypes...)
		}
	}
	switch mode {
	case routePoolProbeRecovery:
		return gatewayruntime.TryStartFaultDomainRecoveryProbe(domain, modelName, requestTypes...)
	case routePoolProbeEmergency:
		return gatewayruntime.TryStartFaultDomainEmergencyRetryProbe(domain, modelName, requestTypes...)
	default:
		return gatewayruntime.TryStartFaultDomainLastResortProbe(domain, modelName, requestTypes...)
	}
}

func releaseRoutePoolChannelProbe(c *gin.Context, channelID int, modelName string, requestTypes ...gatewayruntime.RequestType) {
	if gatewayruntime.IsAutoRouteRequest(c) {
		gatewayruntime.ReleaseUserChannelProbe(c, channelID, modelName, requestTypes...)
		return
	}
	gatewayruntime.ReleaseChannelProbe(channelID, modelName, requestTypes...)
}

func releaseRoutePoolDomainProbe(c *gin.Context, domain, modelName string, requestTypes ...gatewayruntime.RequestType) {
	if gatewayruntime.IsAutoRouteRequest(c) {
		gatewayruntime.ReleaseUserFaultDomainProbe(c, domain, modelName, requestTypes...)
		return
	}
	gatewayruntime.ReleaseFaultDomainProbe(domain, modelName, requestTypes...)
}

func releasePartialRoutePoolProbe(
	c *gin.Context,
	probe *scoredRoutePoolCandidate,
	modelName string,
	channelReady, credentialReady, domainReady bool,
	requestTypes ...gatewayruntime.RequestType,
) {
	if probe == nil || probe.channel == nil {
		return
	}
	if probe.credentialProbe && credentialReady {
		gatewayruntime.ReleaseChannelProbe(probe.channel.Id, "__channel_credentials__")
	}
	if probe.channelProbe && channelReady {
		releaseRoutePoolChannelProbe(c, probe.channel.Id, modelName, requestTypes...)
	}
	if probe.domainProbe && domainReady {
		releaseRoutePoolDomainProbe(c, probe.faultDomain, modelName, requestTypes...)
	}
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
