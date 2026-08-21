package app

import (
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
)

// RetryParam carries group/model selection state across relay retries.
type RetryParam struct {
	Ctx          *gin.Context
	TokenGroup   string
	ModelName    string
	Retry        *int
	resetNextTry bool
}

// EffectiveRetryTimes keeps implicit channel selection resilient when the
// global retry option is left at its legacy zero default. Explicit channel
// requests still stop in shouldRetry; all other relay requests get one bounded
// retry so a transient upstream timeout can move to a healthy channel.
func EffectiveRetryTimes(tokenGroup string) int {
	retryTimes := platformconfig.RetryTimes
	if retryTimes < 0 {
		return 0
	}
	if strings.TrimSpace(tokenGroup) != "" && retryTimes == 0 {
		return 1
	}
	return retryTimes
}

var selectRandomSatisfiedChannel = gatewaystore.GetRandomSatisfiedChannel

func (p *RetryParam) GetRetry() int {
	if p.Retry == nil {
		return 0
	}
	return *p.Retry
}

func (p *RetryParam) SetRetry(retry int) {
	p.Retry = &retry
}

func (p *RetryParam) IncreaseRetry() {
	if p.resetNextTry {
		p.resetNextTry = false
		return
	}
	if p.Retry == nil {
		p.Retry = new(int)
	}
	*p.Retry++
}

func (p *RetryParam) ResetRetryNextTry() {
	p.resetNextTry = true
}

// CacheGetRandomSatisfiedChannel selects an available channel for the current retry round.
func CacheGetRandomSatisfiedChannel(param *RetryParam) (*gatewayschema.Channel, string, error) {
	if param != nil && param.Ctx != nil {
		param.Ctx.Set(routePoolContextKey, RoutePoolSelection{})
		param.Ctx.Set(routePoolFaultDomainContextKey, "")
	}
	var channel *gatewayschema.Channel
	var err error
	selectGroup := param.TokenGroup
	userGroup := httpctx.GetContextKeyString(param.Ctx, constant.ContextKeyUserGroup)

	if param.TokenGroup == AutoGroupName {
		channel, selectGroup, err = selectAutoGroupChannel(param, userGroup)
		if err != nil {
			return nil, selectGroup, err
		}
	} else {
		channel, err = getHealthySatisfiedChannelWithContext(param.Ctx, param.TokenGroup, param.ModelName, param.GetRetry())
		if channel != nil {
			gatewayruntime.SelectRouteDecisionCandidate(param.Ctx, param.TokenGroup, channel.Id, false)
		}
		if err != nil {
			return nil, param.TokenGroup, err
		}
	}
	if channel == nil && requiresOfficialChannel(param.Ctx) {
		httpctx.SetContextKey(param.Ctx, constant.ContextKeyOfficialChannelOnly, false)
		httpctx.SetContextKey(param.Ctx, constant.ContextKeyOfficialChannelFallback, true)
		httpctx.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, 0)
		httpctx.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupRetryIndex, 0)
		param.SetRetry(0)
		return CacheGetRandomSatisfiedChannel(param)
	}
	return channel, selectGroup, nil
}
func getHealthySatisfiedChannel(group string, modelName string, retry int) (*gatewayschema.Channel, error) {
	return getHealthySatisfiedChannelWithContext(nil, group, modelName, retry)
}

func getHealthySatisfiedChannelWithContext(c *gin.Context, group string, modelName string, retry int) (*gatewayschema.Channel, error) {
	return getHealthySatisfiedChannelWithMode(c, group, modelName, retry, true)
}

func getHealthySatisfiedChannelWithMode(c *gin.Context, group string, modelName string, retry int, allowLastResort bool) (*gatewayschema.Channel, error) {
	if channel, managed, err := selectAutomaticPoolChannel(c, group, modelName, retry, allowLastResort); err != nil || managed {
		return channel, err
	}
	var degradedCandidate *gatewayschema.Channel
	var unknownHealthyCandidate *gatewayschema.Channel
	var unknownDegradedCandidate *gatewayschema.Channel
	seenPriorities := make(map[int64]struct{})
	for priorityRetry := retry; priorityRetry < retry+16; priorityRetry++ {
		healthy, degraded, priority, found, err := getHealthySatisfiedChannelAtPriority(c, group, modelName, priorityRetry)
		if err != nil {
			return nil, err
		}
		if !found {
			break
		}
		if _, seen := seenPriorities[priority]; seen {
			break
		}
		seenPriorities[priority] = struct{}{}
		if healthy != nil {
			if remoteCompactionCapabilityRank(c, healthy, modelName) == 0 {
				return healthy, nil
			}
			if unknownHealthyCandidate == nil {
				unknownHealthyCandidate = healthy
			}
			continue
		}
		if degraded != nil {
			if remoteCompactionCapabilityRank(c, degraded, modelName) == 0 && degradedCandidate == nil {
				degradedCandidate = degraded
			} else if unknownDegradedCandidate == nil {
				unknownDegradedCandidate = degraded
			}
		}
	}
	if unknownHealthyCandidate != nil {
		return unknownHealthyCandidate, nil
	}
	if degradedCandidate == nil {
		degradedCandidate = unknownDegradedCandidate
	}
	if degradedCandidate != nil {
		if reserveLegacyCandidateProbe(c, degradedCandidate, modelName, routePoolProbeRecovery) {
			return degradedCandidate, nil
		}
		if !allowLastResort {
			return nil, nil
		}
	} else if !allowLastResort {
		return nil, nil
	}
	return selectLegacyLastResortChannel(c, group, modelName, retry), nil
}

// selectLegacyLastResortChannel keeps one recovery path available when every
// channel for a model is in an active cooldown. It is only called after the
// normal candidate scan found no healthy or degraded route.
func selectLegacyLastResortChannel(c *gin.Context, group, modelName string, retry int) *gatewayschema.Channel {
	requestType := gatewayruntime.RequestTypeFromContext(c)
	for priorityRetry := retry; priorityRetry < retry+16; priorityRetry++ {
		channel, err := selectRandomSatisfiedChannel(group, modelName, priorityRetry)
		if err != nil || channel == nil || channelAlreadyUsed(c, channel.Id) || channelExcludedByScope(c, channel) {
			continue
		}
		if remoteCompactionCapabilityRank(c, channel, modelName) < 0 {
			gatewayruntime.ExcludeRouteDecisionCandidate(c, "remote_compaction_unsupported")
			continue
		}
		now := time.Now()
		health, found := routePoolChannelHealth(c, channel.Id, modelName, requestType)
		domain := gatewayruntime.ChannelFaultDomain(channel.Type, channel.GetBaseURL())
		domainHealth, domainFound := routePoolFaultDomainHealth(c, domain, modelName, requestType)
		if !activeRoutePoolCircuit(health, found, now) && !activeRoutePoolCircuit(domainHealth, domainFound, now) {
			continue
		}
		if reserveLegacyCandidateProbe(c, channel, modelName, routePoolProbeLastResort) && c != nil {
			gatewayruntime.SetRouteDecisionProbeMode(c, gatewayruntime.RouteDecisionProbeLastResort)
		} else if c != nil {
			// A busy recovery lease is not grounds to reject the request. Keep a
			// single best-known route available so all temporary circuits do not
			// become a synchronized 503 wave.
			_ = gatewayruntime.AcquireAllCoolingFallback(c, group, modelName, requestType)
			gatewayruntime.SetRouteDecisionProbeMode(c, gatewayruntime.RouteDecisionProbeLastResort)
		}
		return channel
	}
	return nil
}

func getHealthySatisfiedChannelAtPriority(c *gin.Context, group string, modelName string, retry int) (healthy *gatewayschema.Channel, degraded *gatewayschema.Channel, priority int64, found bool, err error) {
	const maxSelectionAttempts = 16
	requestType := gatewayruntime.RequestTypeFromContext(c)
	var unknownHealthy *gatewayschema.Channel
	var unknownDegraded *gatewayschema.Channel
	for attempt := 0; attempt < maxSelectionAttempts; attempt++ {
		channel, err := selectRandomSatisfiedChannel(group, modelName, retry)
		if err != nil || channel == nil {
			return nil, degraded, priority, found, err
		}
		if retryFallbackChannelID(c) == channel.Id {
			continue
		}
		if channelExcludedByScope(c, channel) {
			continue
		}
		capabilityRank := remoteCompactionCapabilityRank(c, channel, modelName)
		if capabilityRank < 0 {
			gatewayruntime.ExcludeRouteDecisionCandidate(c, "remote_compaction_unsupported")
			continue
		}
		faultDomain := gatewayruntime.ChannelFaultDomain(channel.Type, channel.GetBaseURL())
		if gatewayruntime.IsFaultDomainExcluded(c, faultDomain) {
			continue
		}
		if !found {
			priority = channel.GetPriority()
			found = true
		}
		now := time.Now()
		health, healthFound := routePoolChannelHealth(c, channel.Id, modelName, requestType)
		domainHealth, domainFound := routePoolFaultDomainHealth(c, faultDomain, modelName, requestType)
		if activeRoutePoolCircuit(health, healthFound, now) || activeRoutePoolCircuit(domainHealth, domainFound, now) {
			continue
		}
		if healthFound && (health.State == gatewayruntime.ChannelHealthCooling || health.State == gatewayruntime.ChannelHealthHalfOpen) ||
			domainFound && (domainHealth.State == gatewayruntime.ChannelHealthCooling || domainHealth.State == gatewayruntime.ChannelHealthHalfOpen) {
			if capabilityRank == 0 && degraded == nil {
				// When legacy routing is still in use, an expired circuit may be
				// selected only after all healthy candidates are exhausted. Its
				// next successes are counted as recovery probes by channel health.
				degraded = channel
			} else if capabilityRank > 0 && unknownDegraded == nil {
				unknownDegraded = channel
			}
			continue
		}
		if healthFound && health.State == gatewayruntime.ChannelHealthDegraded {
			if capabilityRank == 0 && degraded == nil {
				degraded = channel
			} else if capabilityRank > 0 && unknownDegraded == nil {
				unknownDegraded = channel
			}
			continue
		}
		if capabilityRank == 0 {
			return channel, degraded, priority, true, nil
		}
		if unknownHealthy == nil {
			unknownHealthy = channel
		}
	}
	if unknownHealthy != nil {
		return unknownHealthy, degraded, priority, found, nil
	}
	if degraded == nil {
		degraded = unknownDegraded
	}
	return nil, degraded, priority, found, nil
}

func requiresOfficialChannel(c *gin.Context) bool {
	return c != nil && httpctx.GetContextKeyBool(c, constant.ContextKeyOfficialChannelOnly)
}

func channelExcludedByScope(c *gin.Context, channel *gatewayschema.Channel) bool {
	return requiresOfficialChannel(c) && channel != nil && !channel.IsOfficial()
}

func retryFallbackChannelID(c *gin.Context) int {
	if c == nil {
		return 0
	}
	return httpctx.GetContextKeyInt(c, constant.ContextKeyRetryFallbackChannelID)
}
