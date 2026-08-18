package app

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewaygroups "github.com/sh2001sh/new-api/internal/gateway/groupsettings"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
)

const autoGroupAttemptedContextKey = "auto_group_attempted_groups"

var hasSelectableAutoGroupRoute = selectableAutoGroupRoute

func selectAutoGroupChannel(param *RetryParam, userGroup string) (*gatewayschema.Channel, string, error) {
	if len(gatewaygroups.GetAutoGroups()) == 0 {
		return nil, AutoGroupName, errors.New("auto groups is not enabled")
	}
	autoGroups := OrderAutoGroups(param.Ctx, userGroup, param.ModelName)
	fallbackGroups := OrderAutoFallbackGroups(param.Ctx, userGroup, param.ModelName, autoGroups)
	candidateGroups := append(append([]string{}, autoGroups...), fallbackGroups...)
	gatewayruntime.UpdateRouteDecisionCandidates(param.Ctx, len(candidateGroups))
	gatewayruntime.MarkRemainingCrossGroupRoutes(param.Ctx, 0)

	for index, group := range candidateGroups {
		if autoGroupWasAttempted(param.Ctx, group) {
			continue
		}
		logger.LogDebug(param.Ctx, "Auto selecting group: %s, priorityRetry: 0", group)
		channel, _ := getHealthySatisfiedChannelWithMode(param.Ctx, group, param.ModelName, 0, false)
		if channel == nil {
			gatewayruntime.ExcludeRouteDecisionCandidate(param.Ctx, "no_healthy_channel")
			httpctx.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, index+1)
			httpctx.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupRetryIndex, 0)
			continue
		}
		bindAutoGroupSelection(param, candidateGroups, index, group, channel)
		if httpctx.GetContextKeyBool(param.Ctx, constant.ContextKeyTokenCrossGroupRetry) && platformconfig.RetryTimes <= 0 {
			httpctx.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, index+1)
			param.SetRetry(0)
			param.ResetRetryNextTry()
		}
		return channel, group, nil
	}
	return selectAutoGroupLastResort(param, candidateGroups)
}

func selectAutoGroupLastResort(param *RetryParam, candidateGroups []string) (*gatewayschema.Channel, string, error) {
	for index, group := range candidateGroups {
		channel, _ := getHealthySatisfiedChannelWithMode(param.Ctx, group, param.ModelName, 0, true)
		if channel == nil {
			continue
		}
		bindAutoGroupSelection(param, candidateGroups, index, group, channel)
		return channel, group, nil
	}
	return nil, AutoGroupName, nil
}

func bindAutoGroupSelection(param *RetryParam, groups []string, index int, group string, channel *gatewayschema.Channel) {
	httpctx.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, index)
	httpctx.SetContextKey(param.Ctx, constant.ContextKeyAutoGroup, group)
	markAutoGroupAttempted(param.Ctx, group)
	markRemainingAutoGroupRoutes(param.Ctx, groups, index, param.ModelName)
	gatewayruntime.SelectRouteDecisionCandidate(param.Ctx, group, channel.Id, false)
	logger.LogDebug(param.Ctx, "Auto selected group: %s", group)
}

func selectableAutoGroupRoute(group, modelName string) (bool, error) {
	detail, err := gatewaystore.LoadEnabledRoutePool(group)
	if err != nil {
		return false, err
	}
	if detail == nil {
		return gatewaystore.HasEnabledChannelForGroupModel(group, modelName), nil
	}
	candidates, err := gatewaystore.LoadRoutePoolCandidates(group, modelName, detail)
	return len(candidates) > 0, err
}

func markRemainingAutoGroupRoutes(c *gin.Context, groups []string, current int, modelName string) {
	for index := current + 1; index < len(groups); index++ {
		if autoGroupWasAttempted(c, groups[index]) {
			continue
		}
		selectable, err := hasSelectableAutoGroupRoute(groups[index], modelName)
		if err != nil {
			logger.LogError(c, "check remaining auto group route failed: "+err.Error())
			gatewayruntime.MarkRemainingCrossGroupRoutes(c, 1)
			return
		}
		if selectable {
			gatewayruntime.MarkRemainingCrossGroupRoutes(c, 1)
			return
		}
	}
	gatewayruntime.MarkRemainingCrossGroupRoutes(c, 0)
}

func markAutoGroupAttempted(c *gin.Context, group string) {
	if c == nil || group == "" || autoGroupWasAttempted(c, group) {
		return
	}
	attempted := append([]string(nil), c.GetStringSlice(autoGroupAttemptedContextKey)...)
	c.Set(autoGroupAttemptedContextKey, append(attempted, group))
}

func autoGroupWasAttempted(c *gin.Context, group string) bool {
	if c == nil || group == "" {
		return false
	}
	for _, attempted := range c.GetStringSlice(autoGroupAttemptedContextKey) {
		if attempted == group {
			return true
		}
	}
	return false
}
