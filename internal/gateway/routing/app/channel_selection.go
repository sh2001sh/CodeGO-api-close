package app

import (
	"errors"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewaygroups "github.com/sh2001sh/new-api/internal/gateway/groupsettings"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	"github.com/sh2001sh/new-api/internal/platform/logger"
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
		if len(gatewaygroups.GetAutoGroups()) == 0 {
			return nil, selectGroup, errors.New("auto groups is not enabled")
		}
		autoGroups := OrderAutoGroups(userGroup, param.ModelName)
		fallbackGroups := OrderAutoFallbackGroups(userGroup, param.ModelName, autoGroups)
		candidateGroups := append(append([]string{}, autoGroups...), fallbackGroups...)
		gatewayruntime.UpdateRouteDecisionCandidates(param.Ctx, len(candidateGroups))
		startGroupIndex := 0
		crossGroupRetry := httpctx.GetContextKeyBool(param.Ctx, constant.ContextKeyTokenCrossGroupRetry)

		if lastGroupIndex, exists := httpctx.GetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex); exists {
			if idx, ok := lastGroupIndex.(int); ok {
				startGroupIndex = idx
				if param.GetRetry() > 0 {
					// Auto routing is ordered by effective user cost. Once a group
					// has failed, continue with the next cheapest healthy group
					// instead of retrying the same group and hiding available
					// capacity behind its transient failure.
					startGroupIndex++
				}
			}
		}

		for i := startGroupIndex; i < len(candidateGroups); i++ {
			autoGroup := candidateGroups[i]
			priorityRetry := param.GetRetry()
			if i > startGroupIndex {
				priorityRetry = 0
			}
			logger.LogDebug(param.Ctx, "Auto selecting group: %s, priorityRetry: %d", autoGroup, priorityRetry)

			channel, _ = getHealthySatisfiedChannelWithMode(param.Ctx, autoGroup, param.ModelName, priorityRetry, false)
			if channel == nil {
				gatewayruntime.ExcludeRouteDecisionCandidate(param.Ctx, "no_healthy_channel")
				logger.LogDebug(param.Ctx, "No available channel in group %s for model %s at priorityRetry %d, trying next group", autoGroup, param.ModelName, priorityRetry)
				httpctx.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i+1)
				httpctx.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupRetryIndex, 0)
				param.SetRetry(0)
				continue
			}
			httpctx.SetContextKey(param.Ctx, constant.ContextKeyAutoGroup, autoGroup)
			selectGroup = autoGroup
			gatewayruntime.SelectRouteDecisionCandidate(param.Ctx, autoGroup, channel.Id, false)
			logger.LogDebug(param.Ctx, "Auto selected group: %s", autoGroup)

			if crossGroupRetry && priorityRetry >= platformconfig.RetryTimes {
				logger.LogDebug(param.Ctx, "Current group %s retries exhausted (priorityRetry=%d >= RetryTimes=%d), preparing switch to next group for next retry", autoGroup, priorityRetry, platformconfig.RetryTimes)
				httpctx.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i+1)
				param.SetRetry(0)
				param.ResetRetryNextTry()
			} else {
				httpctx.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i)
			}
			break
		}
		// Only after every automatic group has no healthy candidate may a
		// cooling channel be used as a last-resort recovery probe. This keeps a
		// temporarily bad cheap group from masking a healthy fallback group.
		if channel == nil {
			for i, autoGroup := range candidateGroups {
				channel, _ = getHealthySatisfiedChannelWithMode(param.Ctx, autoGroup, param.ModelName, 0, true)
				if channel == nil {
					continue
				}
				httpctx.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i)
				httpctx.SetContextKey(param.Ctx, constant.ContextKeyAutoGroup, autoGroup)
				selectGroup = autoGroup
				gatewayruntime.SelectRouteDecisionCandidate(param.Ctx, autoGroup, channel.Id, false)
				break
			}
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
			return healthy, nil
		}
		if degradedCandidate == nil && degraded != nil {
			degradedCandidate = degraded
		}
	}
	if degradedCandidate != nil || !allowLastResort {
		return degradedCandidate, nil
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
		health, found := gatewayruntime.GetChannelHealth(channel.Id, modelName, requestType)
		if !found || health.State != gatewayruntime.ChannelHealthCooling || !health.CoolingUntil.After(time.Now()) {
			continue
		}
		if gatewayruntime.TryStartChannelLastResortProbe(channel.Id, modelName, requestType) && c != nil {
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
		faultDomain := gatewayruntime.ChannelFaultDomain(channel.Type, channel.GetBaseURL())
		if gatewayruntime.IsFaultDomainExcluded(c, faultDomain) {
			continue
		}
		if !found {
			priority = channel.GetPriority()
			found = true
		}
		health, healthFound := gatewayruntime.GetChannelHealth(channel.Id, modelName, requestType)
		if healthFound && health.State == gatewayruntime.ChannelHealthCooling {
			if !health.CoolingUntil.After(time.Now()) && degraded == nil {
				// When legacy routing is still in use, an expired circuit may be
				// selected only after all healthy candidates are exhausted. Its
				// next successes are counted as recovery probes by channel health.
				degraded = channel
			}
			continue
		}
		if healthFound && health.State == gatewayruntime.ChannelHealthDegraded {
			if degraded == nil {
				degraded = channel
			}
			continue
		}
		return channel, degraded, priority, true, nil
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
