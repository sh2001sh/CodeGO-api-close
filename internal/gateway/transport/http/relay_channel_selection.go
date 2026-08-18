package http

import (
	"errors"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewayexecutionapp "github.com/sh2001sh/new-api/internal/gateway/execution/app"
	"github.com/sh2001sh/new-api/internal/gateway/routepin"
	gatewayroutingapp "github.com/sh2001sh/new-api/internal/gateway/routing/app"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	marketplaceapp "github.com/sh2001sh/new-api/internal/marketplace/app"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/sh2001sh/new-api/types"
)

var selectUnifiedAutoRetryChannel = gatewayroutingapp.CacheGetRandomSatisfiedChannel

func getChannel(c *gin.Context, info *gatewayruntime.RelayInfo, retryParam *gatewayroutingapp.RetryParam) (*gatewayschema.Channel, *types.NewAPIError) {
	if pin, pinned := routepin.FromContext(c); pinned {
		return loadPinnedChannel(c, info, pin.ChannelID)
	}
	if info.ChannelMeta == nil {
		return selectedDistributorChannel(c), nil
	}
	if channel, handled, channelErr := nextUnifiedAutoChannel(c, info, retryParam); handled {
		return channel, channelErr
	}
	return selectRelayRetryChannel(c, info, retryParam)
}

func loadPinnedChannel(c *gin.Context, info *gatewayruntime.RelayInfo, channelID int) (*gatewayschema.Channel, *types.NewAPIError) {
	channel, err := gatewaystore.LoadChannelByID(channelID, true)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	if setupErr := gatewayexecutionapp.SetupContextForSelectedChannel(c, channel, info.OriginModelName); setupErr != nil {
		return nil, setupErr
	}
	applyRoutePoolSelection(c, info)
	return channel, nil
}

func selectedDistributorChannel(c *gin.Context) *gatewayschema.Channel {
	autoBanInt := 0
	if c.GetBool("auto_ban") {
		autoBanInt = 1
	}
	return &gatewayschema.Channel{
		Id: c.GetInt("channel_id"), Type: c.GetInt("channel_type"),
		Name: c.GetString("channel_name"), AutoBan: &autoBanInt,
	}
}

func selectRelayRetryChannel(c *gin.Context, info *gatewayruntime.RelayInfo, retryParam *gatewayroutingapp.RetryParam) (*gatewayschema.Channel, *types.NewAPIError) {
	c.Set(routeSelectionExhaustedContextKey, false)
	channel, selectGroup, err := gatewayroutingapp.CacheGetRandomSatisfiedChannel(retryParam)
	if err == nil && channel == nil {
		channel, selectGroup = retryFallbackChannel(c, retryParam, selectGroup)
		if channel == nil {
			channel, selectGroup = retryLastUsedSoleRoute(c, retryParam, selectGroup)
		}
	}
	applyRoutePoolSelection(c, info)
	info.PriceData.GroupRatioInfo = gatewayruntime.HandleGroupRatio(c, info)
	if err != nil {
		return nil, noRetryChannelError(c, selectGroup, info.OriginModelName, err)
	}
	if channel == nil {
		return nil, noRetryChannelError(c, selectGroup, info.OriginModelName, nil)
	}
	if setupErr := gatewayexecutionapp.SetupContextForSelectedChannel(c, channel, info.OriginModelName); setupErr != nil {
		return nil, setupErr
	}
	return channel, nil
}

func applyRoutePoolSelection(c *gin.Context, info *gatewayruntime.RelayInfo) {
	if selection, found := gatewayroutingapp.GetRoutePoolSelection(c); found {
		info.RoutePoolID = selection.PoolID
		info.ProcurementCostMultiplier = selection.ProcurementCostMultiplier
	}
}

func noRetryChannelError(c *gin.Context, group, modelName string, cause error) *types.NewAPIError {
	c.Set(routeSelectionExhaustedContextKey, true)
	gatewayruntime.ExcludeRouteDecisionCandidate(c, "no_selectable_candidate")
	message := fmt.Sprintf("分组 %s 下模型 %s 的可用渠道不存在（retry）", group, modelName)
	if cause != nil {
		message = fmt.Sprintf("获取分组 %s 下模型 %s 的可用渠道失败（retry）：%s", group, modelName, cause.Error())
	}
	return types.NewError(errors.New(message), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
}

func nextUnifiedAutoChannel(c *gin.Context, info *gatewayruntime.RelayInfo, retryParam *gatewayroutingapp.RetryParam) (*gatewayschema.Channel, bool, *types.NewAPIError) {
	if c == nil || info == nil || retryParam == nil || retryParam.GetRetry() <= 0 {
		return nil, false, nil
	}
	bindings, found := httpctx.GetContextKeyType[[]marketplaceapp.RoutingBinding](c, constant.ContextKeyUnifiedAutoBindings)
	if !found || len(bindings) == 0 {
		return nil, false, nil
	}
	start := httpctx.GetContextKeyInt(c, constant.ContextKeyUnifiedAutoIndex) + 1
	for index := start; index < len(bindings); index++ {
		binding := bindings[index]
		candidateRetry := 0
		channel, _, err := selectUnifiedAutoRetryChannel(&gatewayroutingapp.RetryParam{
			Ctx: c, TokenGroup: binding.InternalGroup, ModelName: info.OriginModelName, Retry: &candidateRetry,
		})
		if err != nil || channel == nil {
			gatewayruntime.ExcludeRouteDecisionCandidate(c, "unified_auto_unavailable")
			continue
		}
		httpctx.SetContextKey(c, constant.ContextKeyUnifiedAutoIndex, index)
		applyUnifiedAutoBinding(c, info, binding)
		retryParam.TokenGroup = binding.InternalGroup
		gatewayruntime.MarkRemainingCrossGroupRoutes(c, len(bindings)-index-1)
		if setupErr := gatewayexecutionapp.SetupContextForSelectedChannel(c, channel, info.OriginModelName); setupErr != nil {
			gatewayruntime.ExcludeRouteDecisionCandidate(c, "unified_auto_setup_failed")
			continue
		}
		return channel, true, nil
	}
	gatewayruntime.MarkRemainingCrossGroupRoutes(c, 0)
	c.Set(routeSelectionExhaustedContextKey, true)
	return nil, true, types.NewError(
		errors.New("Auto 路由池中的分组均已尝试且当前不可用"),
		types.ErrorCodeGetChannelFailed,
		types.ErrOptionWithSkipRetry(),
	)
}

func applyUnifiedAutoBinding(c *gin.Context, info *gatewayruntime.RelayInfo, binding marketplaceapp.RoutingBinding) {
	httpctx.SetContextKey(c, constant.ContextKeyUsingGroup, binding.InternalGroup)
	httpctx.SetContextKey(c, constant.ContextKeyTokenGroup, binding.InternalGroup)
	info.UsingGroup = binding.InternalGroup
	info.TokenGroup = binding.InternalGroup
	if binding.SourceType == marketplacedomain.SourceTypeMarketplaceUser {
		applyMarketplaceBinding(c, info, binding)
		return
	}
	httpctx.SetContextKey(c, constant.ContextKeyMarketplaceGroupID, "")
	httpctx.SetContextKey(c, constant.ContextKeyMarketplaceOwnerID, 0)
	httpctx.SetContextKey(c, constant.ContextKeyMarketplaceSourceType, "")
	httpctx.SetContextKey(c, constant.ContextKeyMarketplaceCreditPolicy, "")
	httpctx.SetContextKey(c, constant.ContextKeyMarketplaceMultiplier, float64(0))
	httpctx.SetContextKey(c, constant.ContextKeyMarketplaceModelPrices, map[string]marketplaceapp.ChannelModelPrice{})
	info.MarketplaceGroupID = ""
	info.MarketplaceOwnerID = 0
	info.MarketplaceSourceType = ""
	info.MarketplaceCreditPolicy = ""
	info.MarketplaceMultiplier = 0
}

func applyMarketplaceBinding(c *gin.Context, info *gatewayruntime.RelayInfo, binding marketplaceapp.RoutingBinding) {
	httpctx.SetContextKey(c, constant.ContextKeyMarketplaceGroupID, binding.GroupID)
	httpctx.SetContextKey(c, constant.ContextKeyMarketplaceOwnerID, binding.OwnerUserID)
	httpctx.SetContextKey(c, constant.ContextKeyMarketplaceSourceType, binding.SourceType)
	httpctx.SetContextKey(c, constant.ContextKeyMarketplaceCreditPolicy, binding.CreditPoolPolicy)
	httpctx.SetContextKey(c, constant.ContextKeyMarketplaceMultiplier, binding.Multiplier)
	httpctx.SetContextKey(c, constant.ContextKeyMarketplaceModelPrices, binding.ModelPrices)
	info.MarketplaceGroupID = binding.GroupID
	info.MarketplaceOwnerID = binding.OwnerUserID
	info.MarketplaceSourceType = binding.SourceType
	info.MarketplaceCreditPolicy = binding.CreditPoolPolicy
	info.MarketplaceMultiplier = binding.Multiplier
}
