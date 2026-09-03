package middleware

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewayroutingapp "github.com/sh2001sh/new-api/internal/gateway/routing/app"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	marketplaceapp "github.com/sh2001sh/new-api/internal/marketplace/app"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
)

// selectMarketplaceAutoChannel resolves a user-managed group pool only after
// the request model is known. It returns managed=false for every normal group.
func selectMarketplaceAutoChannel(c *gin.Context, tokenGroup, modelName string) (*gatewayschema.Channel, string, bool, error) {
	isLegacyMarketplaceAuto := marketplaceapp.IsMarketplaceAutoTokenGroup(tokenGroup)
	isNamedMarketplacePool := marketplaceapp.IsMarketplaceRoutePoolTokenGroup(tokenGroup)
	isGlobalAuto := tokenGroup == gatewayroutingapp.AutoGroupName
	if !isLegacyMarketplaceAuto && !isNamedMarketplacePool && !isGlobalAuto {
		return nil, tokenGroup, false, nil
	}
	gatewayruntime.MarkAutoRouteRequest(c)
	userID := httpctx.GetContextKeyInt(c, constant.ContextKeyUserId)
	if isGlobalAuto && !marketplaceapp.HasConfiguredAutoRoutePool(userID) {
		return nil, tokenGroup, false, nil
	}
	multiplierLimit := httpctx.GetContextKeyFloat64(c, constant.ContextKeyTokenMarketplaceMultiplierLimit)
	var bindings []marketplaceapp.RoutingBinding
	var err error
	if isNamedMarketplacePool {
		bindings, err = marketplaceapp.ResolveRoutePoolBindings(userID, marketplaceapp.RoutePoolIDFromTokenGroup(tokenGroup), modelName, multiplierLimit)
	} else {
		bindings, err = marketplaceapp.ResolveAutoRouteBindings(userID, modelName, multiplierLimit)
	}
	if err != nil {
		return nil, tokenGroup, true, err
	}
	httpctx.SetContextKey(c, constant.ContextKeyUnifiedAutoBindings, bindings)
	httpctx.SetContextKey(c, constant.ContextKeyUnifiedAutoIndex, -1)
	gatewayruntime.UpdateRouteDecisionCandidates(c, len(bindings))
	for index, binding := range bindings {
		candidateOrder := index + 1
		retry := 0
		channel, _, selectErr := gatewayroutingapp.CacheGetRandomSatisfiedChannel(&gatewayroutingapp.RetryParam{
			Ctx: c, TokenGroup: binding.InternalGroup, ModelName: modelName, Retry: &retry,
		})
		if selectErr != nil {
			gatewayruntime.RecordRouteDecisionCandidate(c, candidateOrder, binding.InternalGroup, "skipped", "selection_error", 0)
			gatewayruntime.ExcludeRouteDecisionCandidate(c, "unified_auto_select_error")
			continue
		}
		if channel == nil {
			gatewayruntime.RecordRouteDecisionCandidate(c, candidateOrder, binding.InternalGroup, "skipped", "no_healthy_channel", 0)
			gatewayruntime.ExcludeRouteDecisionCandidate(c, "marketplace_auto_unavailable")
			continue
		}
		gatewayruntime.RecordRouteDecisionCandidate(c, candidateOrder, binding.InternalGroup, "selected", "preflight_ok", channel.Id)
		applyMarketplaceAutoBinding(c, binding)
		httpctx.SetContextKey(c, constant.ContextKeyUnifiedAutoIndex, index)
		for remaining := index + 1; remaining < len(bindings); remaining++ {
			gatewayruntime.RecordRouteDecisionCandidate(c, remaining+1, bindings[remaining].InternalGroup, "not_attempted", "lower_priority_fallback", 0)
		}
		gatewayruntime.MarkRemainingCrossGroupRoutes(c, len(bindings)-index-1)
		return channel, binding.InternalGroup, true, nil
	}
	gatewayruntime.MarkRemainingCrossGroupRoutes(c, 0)
	return nil, tokenGroup, true, errors.New("全局 Auto 路由池当前没有可用渠道")
}

func applyMarketplaceAutoBinding(c *gin.Context, binding marketplaceapp.RoutingBinding) {
	httpctx.SetContextKey(c, constant.ContextKeyUsingGroup, binding.InternalGroup)
	httpctx.SetContextKey(c, constant.ContextKeyTokenGroup, binding.InternalGroup)
	if binding.SourceType == marketplacedomain.SourceTypeMarketplaceUser {
		httpctx.SetContextKey(c, constant.ContextKeyMarketplaceGroupID, binding.GroupID)
		httpctx.SetContextKey(c, constant.ContextKeyMarketplaceOwnerID, binding.OwnerUserID)
		httpctx.SetContextKey(c, constant.ContextKeyMarketplaceSourceType, binding.SourceType)
		httpctx.SetContextKey(c, constant.ContextKeyMarketplaceCreditPolicy, binding.CreditPoolPolicy)
		httpctx.SetContextKey(c, constant.ContextKeyMarketplaceMultiplier, binding.Multiplier)
		httpctx.SetContextKey(c, constant.ContextKeyMarketplaceModelPrices, binding.ModelPrices)
		return
	}
	httpctx.SetContextKey(c, constant.ContextKeyMarketplaceGroupID, "")
	httpctx.SetContextKey(c, constant.ContextKeyMarketplaceOwnerID, 0)
	httpctx.SetContextKey(c, constant.ContextKeyMarketplaceSourceType, "")
	httpctx.SetContextKey(c, constant.ContextKeyMarketplaceCreditPolicy, "")
	httpctx.SetContextKey(c, constant.ContextKeyMarketplaceMultiplier, float64(0))
	httpctx.SetContextKey(c, constant.ContextKeyMarketplaceModelPrices, map[string]marketplaceapp.ChannelModelPrice{})
}
