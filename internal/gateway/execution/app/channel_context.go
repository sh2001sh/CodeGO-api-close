package app

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewaydomain "github.com/sh2001sh/new-api/internal/gateway/domain"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/sh2001sh/new-api/types"
)

// SetupContextForSelectedChannel writes selected-channel metadata into the request context.
func SetupContextForSelectedChannel(c *gin.Context, channel *gatewayschema.Channel, modelName string) *types.NewAPIError {
	c.Set("original_model", modelName)
	if channel == nil {
		return types.NewError(errors.New("channel is nil"), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}

	httpctx.SetContextKey(c, constant.ContextKeyChannelId, channel.Id)
	httpctx.SetContextKey(c, constant.ContextKeyChannelScope, normalizedChannelScope(channel))
	httpctx.SetContextKey(c, constant.ContextKeyChannelName, channel.Name)
	httpctx.SetContextKey(c, constant.ContextKeyChannelType, channel.Type)
	httpctx.SetContextKey(c, constant.ContextKeyChannelCreateTime, channel.CreatedTime)
	httpctx.SetContextKey(c, constant.ContextKeyChannelSetting, gatewaydomain.GetSettings(channel))
	httpctx.SetContextKey(c, constant.ContextKeyChannelOtherSetting, gatewaydomain.GetOtherSettings(channel))

	paramOverride := gatewaydomain.GetParamOverride(channel)
	headerOverride := gatewaydomain.GetHeaderOverride(channel)
	if mergedParam, applied := gatewayruntime.ApplyChannelAffinityOverrideTemplate(c, paramOverride); applied {
		paramOverride = mergedParam
	}
	httpctx.SetContextKey(c, constant.ContextKeyChannelParamOverride, paramOverride)
	httpctx.SetContextKey(c, constant.ContextKeyChannelHeaderOverride, headerOverride)

	if channel.OpenAIOrganization != nil && *channel.OpenAIOrganization != "" {
		httpctx.SetContextKey(c, constant.ContextKeyChannelOrganization, *channel.OpenAIOrganization)
	}
	httpctx.SetContextKey(c, constant.ContextKeyChannelAutoBan, channel.GetAutoBan())
	httpctx.SetContextKey(c, constant.ContextKeyChannelModelMapping, channel.GetModelMapping())
	httpctx.SetContextKey(c, constant.ContextKeyChannelStatusCodeMapping, channel.GetStatusCodeMapping())

	key, index, newAPIError := selectChannelKeyForRequest(c, channel)
	if newAPIError != nil {
		return newAPIError
	}
	if channel.ChannelInfo.IsMultiKey {
		httpctx.SetContextKey(c, constant.ContextKeyChannelIsMultiKey, true)
		httpctx.SetContextKey(c, constant.ContextKeyChannelMultiKeyIndex, index)
	} else {
		httpctx.SetContextKey(c, constant.ContextKeyChannelIsMultiKey, false)
	}

	httpctx.SetContextKey(c, constant.ContextKeyChannelKey, key)
	httpctx.SetContextKey(c, constant.ContextKeyChannelBaseUrl, channel.GetBaseURL())
	faultDomain := strings.TrimSpace(c.GetString("automatic_route_pool_fault_domain"))
	if faultDomain == "" {
		faultDomain = gatewayruntime.ChannelFaultDomain(channel.Type, channel.GetBaseURL())
	}
	// A retry can choose a different channel in the same Gin context. Always
	// refresh the effective domain so capacity and cooldowns follow that channel.
	c.Set("channel_fault_domain", faultDomain)
	classifySelectedChannelRoute(c, channel.Id, modelName)
	httpctx.SetContextKey(c, constant.ContextKeySystemPromptOverride, false)

	switch channel.Type {
	case constant.ChannelTypeAzure:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeVertexAi:
		c.Set("region", channel.Other)
	case constant.ChannelTypeXunfei:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeGemini:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeAli:
		c.Set("plugin", channel.Other)
	case constant.ChannelCloudflare:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeMokaAI:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeCoze:
		c.Set("bot_id", channel.Other)
	}
	return nil
}

func classifySelectedChannelRoute(c *gin.Context, channelID int, modelName string) {
	profile, found := gatewayruntime.RequestProfileFromContext(c)
	if !found || !profile.IsStream || profile.Protocol != string(types.RelayFormatOpenAIResponses) {
		return
	}
	if gatewayruntime.HasRemainingCrossGroupRoute(c) {
		gatewayruntime.MarkSingleChannelRoute(c, false)
		return
	}
	alternative, err := hasAlternativeSelectableRoute(channelID, selectedChannelGroup(c), modelName)
	if err != nil {
		platformobservability.SysError("classify selected channel route failed: " + err.Error())
		gatewayruntime.MarkSingleChannelRoute(c, false)
		return
	}
	gatewayruntime.MarkSingleChannelRoute(c, !alternative)
}

func normalizedChannelScope(channel *gatewayschema.Channel) string {
	if channel != nil && !channel.IsOfficial() {
		return gatewayschema.ChannelScopeExternal
	}
	return gatewayschema.ChannelScopeOfficial
}
