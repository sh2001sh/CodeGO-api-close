package http

import (
	"errors"
	"fmt"
	platformtext "github.com/sh2001sh/new-api/internal/platform/textx"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/samber/lo"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	auditprojection "github.com/sh2001sh/new-api/internal/audit/projection"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	gatewayexecutionapp "github.com/sh2001sh/new-api/internal/gateway/execution/app"
	routepin "github.com/sh2001sh/new-api/internal/gateway/routepin"
	gatewayroutingapp "github.com/sh2001sh/new-api/internal/gateway/routing/app"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	gatewaystream "github.com/sh2001sh/new-api/internal/gateway/stream"
	marketplaceapp "github.com/sh2001sh/new-api/internal/marketplace/app"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	"github.com/sh2001sh/new-api/types"
)

func relayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	return gatewayexecutionapp.ExecuteRelay(c, info)
}

func geminiRelayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	if strings.Contains(c.Request.URL.Path, "embed") {
		return gatewayexecutionapp.ExecuteGeminiEmbeddingRelay(c, info)
	}
	return gatewayexecutionapp.ExecuteGeminiRelay(c, info)
}

var relayUpgrader = websocket.Upgrader{
	Subprotocols: []string{"realtime"},
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

const (
	routeSelectionExhaustedContextKey = "route_selection_exhausted"
	maxRouteSelectionWaitRetries      = 2
	routeSelectionRetryDelay          = 500 * time.Millisecond
)

func addUsedChannel(c *gin.Context, channelID int) {
	useChannel := c.GetStringSlice("use_channel")
	useChannel = append(useChannel, fmt.Sprintf("%d", channelID))
	c.Set("use_channel", useChannel)
}

func fastTokenCountMetaForPricing(request dto.Request) *types.TokenCountMeta {
	if request == nil {
		return &types.TokenCountMeta{}
	}
	meta := &types.TokenCountMeta{TokenType: types.TokenTypeTokenizer}
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		maxCompletionTokens := lo.FromPtrOr(r.MaxCompletionTokens, uint(0))
		maxTokens := lo.FromPtrOr(r.MaxTokens, uint(0))
		if maxCompletionTokens > maxTokens {
			meta.MaxTokens = int(maxCompletionTokens)
		} else {
			meta.MaxTokens = int(maxTokens)
		}
	case *dto.OpenAIResponsesRequest:
		meta.MaxTokens = int(lo.FromPtrOr(r.MaxOutputTokens, uint(0)))
	case *dto.ClaudeRequest:
		meta.MaxTokens = int(lo.FromPtr(r.MaxTokens))
	case *dto.ImageRequest:
		return r.GetTokenCountMeta()
	}
	return meta
}

func getChannel(c *gin.Context, info *relaycommon.RelayInfo, retryParam *gatewayroutingapp.RetryParam) (*gatewayschema.Channel, *types.NewAPIError) {
	if pin, pinned := routepin.FromContext(c); pinned {
		channel, err := gatewaystore.LoadChannelByID(pin.ChannelID, true)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
		}
		if setupErr := gatewayexecutionapp.SetupContextForSelectedChannel(c, channel, info.OriginModelName); setupErr != nil {
			return nil, setupErr
		}
		if selection, found := gatewayroutingapp.GetRoutePoolSelection(c); found {
			info.RoutePoolID = selection.PoolID
			info.ProcurementCostMultiplier = selection.ProcurementCostMultiplier
		}
		return channel, nil
	}
	if info.ChannelMeta == nil {
		autoBan := c.GetBool("auto_ban")
		autoBanInt := 1
		if !autoBan {
			autoBanInt = 0
		}
		return &gatewayschema.Channel{
			Id:      c.GetInt("channel_id"),
			Type:    c.GetInt("channel_type"),
			Name:    c.GetString("channel_name"),
			AutoBan: &autoBanInt,
		}, nil
	}
	if channel, handled, channelErr := nextUnifiedAutoChannel(c, info, retryParam); handled {
		return channel, channelErr
	}

	c.Set(routeSelectionExhaustedContextKey, false)
	channel, selectGroup, err := gatewayroutingapp.CacheGetRandomSatisfiedChannel(retryParam)
	if err == nil && channel == nil {
		channel, selectGroup = retryFallbackChannel(c, retryParam, selectGroup)
		if channel == nil {
			channel, selectGroup = retryLastUsedSoleRoute(c, retryParam, selectGroup)
		}
	}
	if selection, found := gatewayroutingapp.GetRoutePoolSelection(c); found {
		info.RoutePoolID = selection.PoolID
		info.ProcurementCostMultiplier = selection.ProcurementCostMultiplier
	}
	info.PriceData.GroupRatioInfo = relaycommon.HandleGroupRatio(c, info)
	if err != nil {
		c.Set(routeSelectionExhaustedContextKey, true)
		gatewayruntime.ExcludeRouteDecisionCandidate(c, "no_selectable_candidate")
		return nil, types.NewError(
			fmt.Errorf("获取分组 %s 下模型 %s 的可用渠道失败（retry）：%s", selectGroup, info.OriginModelName, err.Error()),
			types.ErrorCodeGetChannelFailed,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if channel == nil {
		c.Set(routeSelectionExhaustedContextKey, true)
		gatewayruntime.ExcludeRouteDecisionCandidate(c, "no_selectable_candidate")
		return nil, types.NewError(
			fmt.Errorf("分组 %s 下模型 %s 的可用渠道不存在（retry）", selectGroup, info.OriginModelName),
			types.ErrorCodeGetChannelFailed,
			types.ErrOptionWithSkipRetry(),
		)
	}

	newAPIError := gatewayexecutionapp.SetupContextForSelectedChannel(c, channel, info.OriginModelName)
	if newAPIError != nil {
		return nil, newAPIError
	}
	return channel, nil
}

func nextUnifiedAutoChannel(c *gin.Context, info *relaycommon.RelayInfo, retryParam *gatewayroutingapp.RetryParam) (*gatewayschema.Channel, bool, *types.NewAPIError) {
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
		channel, _, err := gatewayroutingapp.CacheGetRandomSatisfiedChannel(&gatewayroutingapp.RetryParam{
			Ctx: c, TokenGroup: binding.InternalGroup, ModelName: info.OriginModelName, Retry: &candidateRetry,
		})
		if err != nil || channel == nil {
			gatewayruntime.ExcludeRouteDecisionCandidate(c, "unified_auto_unavailable")
			continue
		}
		httpctx.SetContextKey(c, constant.ContextKeyUnifiedAutoIndex, index)
		applyUnifiedAutoBinding(c, info, binding)
		retryParam.TokenGroup = binding.InternalGroup
		if setupErr := gatewayexecutionapp.SetupContextForSelectedChannel(c, channel, info.OriginModelName); setupErr != nil {
			gatewayruntime.ExcludeRouteDecisionCandidate(c, "unified_auto_setup_failed")
			continue
		}
		return channel, true, nil
	}
	c.Set(routeSelectionExhaustedContextKey, true)
	return nil, true, types.NewError(
		errors.New("Auto 路由池中的分组均已尝试且当前不可用"),
		types.ErrorCodeGetChannelFailed,
		types.ErrOptionWithSkipRetry(),
	)
}

func applyUnifiedAutoBinding(c *gin.Context, info *relaycommon.RelayInfo, binding marketplaceapp.RoutingBinding) {
	httpctx.SetContextKey(c, constant.ContextKeyUsingGroup, binding.InternalGroup)
	httpctx.SetContextKey(c, constant.ContextKeyTokenGroup, binding.InternalGroup)
	info.UsingGroup = binding.InternalGroup
	info.TokenGroup = binding.InternalGroup
	if binding.SourceType == marketplacedomain.SourceTypeMarketplaceUser {
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

func retryFallbackChannel(c *gin.Context, retryParam *gatewayroutingapp.RetryParam, selectGroup string) (*gatewayschema.Channel, string) {
	if c == nil || retryParam == nil || retryParam.GetRetry() <= 0 {
		return nil, selectGroup
	}
	channelID := httpctx.GetContextKeyInt(c, constant.ContextKeyRetryFallbackChannelID)
	if channelID <= 0 {
		return nil, selectGroup
	}
	httpctx.SetContextKey(c, constant.ContextKeyRetryFallbackChannelID, 0)
	channel, err := gatewaystore.GetCachedChannel(channelID)
	if err != nil || channel == nil {
		return nil, selectGroup
	}
	if retryParam.TokenGroup == gatewayroutingapp.AutoGroupName {
		if selected := httpctx.GetContextKeyString(c, constant.ContextKeyAutoGroup); selected != "" {
			selectGroup = selected
		}
	}
	// The first attempt already established group/model ability. Do not query the
	// ability cache again here: a cache refresh between attempts can otherwise
	// reject the very sole route that was just selected and turn its real
	// upstream error into a misleading "no available channel" response. A manual
	// global channel disable is still honored.
	if !isRetryChannelReusable(channel) {
		return nil, selectGroup
	}
	gatewayruntime.SelectRouteDecisionCandidate(c, selectGroup, channelID, false)
	return channel, selectGroup
}

// retryLastUsedSoleRoute is the final guard against retry de-duplication
// turning a transient failure on a sole route into "no available channel".
// It is reachable only on the second attempt before any downstream content is
// delivered, and revalidates the channel's current enabled ability.
func retryLastUsedSoleRoute(c *gin.Context, retryParam *gatewayroutingapp.RetryParam, selectGroup string) (*gatewayschema.Channel, string) {
	if c == nil || retryParam == nil || retryParam.GetRetry() <= 0 ||
		httpctx.GetContextKeyBool(c, constant.ContextKeyResponseBodyDelivered) ||
		c.GetBool(string(constant.ContextKeyStreamContentDelivered)) {
		return nil, selectGroup
	}
	usedChannels := c.GetStringSlice("use_channel")
	if len(usedChannels) != 1 {
		return nil, selectGroup
	}
	channelID, err := strconv.Atoi(strings.TrimSpace(usedChannels[0]))
	if err != nil || channelID <= 0 {
		return nil, selectGroup
	}
	channel, err := gatewaystore.GetCachedChannel(channelID)
	if err != nil || !isRetryChannelReusable(channel) {
		return nil, selectGroup
	}
	gatewayruntime.SelectRouteDecisionCandidate(c, selectGroup, channelID, false)
	return channel, selectGroup
}

func isRetryChannelReusable(channel *gatewayschema.Channel) bool {
	return channel != nil && channel.Status == constant.ChannelStatusEnabled
}

func shouldWaitForRouteSelection(c *gin.Context, attempt int) bool {
	if budget := gatewayruntime.RequestBudgetFromContext(c); budget != nil && budget.Remaining(time.Now()) <= routeSelectionRetryDelay {
		return false
	}
	return c != nil && c.Request != nil &&
		!c.GetBool(string(constant.ContextKeyClientGone)) &&
		c.GetBool(routeSelectionExhaustedContextKey) &&
		attempt < maxRouteSelectionWaitRetries
}

func waitForRouteSelection(c *gin.Context, attempt int) bool {
	delay := time.Duration(attempt+1) * routeSelectionRetryDelay
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-c.Request.Context().Done():
		return false
	}
}

func shouldRetry(c *gin.Context, openaiErr *types.NewAPIError, retryTimes int) bool {
	if openaiErr == nil {
		return false
	}
	if c != nil && c.GetBool(string(constant.ContextKeyClientGone)) {
		return false
	}
	if gatewayruntime.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		return false
	}
	if retryTimes <= 0 {
		return false
	}
	if budget := gatewayruntime.RequestBudgetFromContext(c); budget != nil && !budget.CanRetry(time.Now()) {
		gatewayruntime.ExcludeRouteDecisionCandidate(c, "request_budget_exhausted")
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	if httpctx.GetContextKeyBool(c, constant.ContextKeyResponseBodyDelivered) &&
		!canRetryResponsesStreamBeforeContent(c) {
		return false
	}
	// Responses lifecycle frames do not contain model-visible output. A stalled
	// upstream may emit those frames long before it fails, so keep the retry
	// path available until semantic content is delivered.
	if !canRetryResponsesStreamBeforeContent(c) && !withinGPTRetryFailureWindow(c) {
		return false
	}
	if types.IsChannelError(openaiErr) {
		return true
	}
	if types.IsSkipRetryError(openaiErr) {
		return false
	}
	if gatewayexecutionapp.IsModelScopedUpstreamFailure(openaiErr) {
		return c.GetBool("model_unavailable_with_alternative")
	}
	if gatewayexecutionapp.IsUpstreamCredentialRejectedError(openaiErr) {
		return true
	}
	code := openaiErr.StatusCode
	if code >= 200 && code < 300 {
		return false
	}
	if code < 100 || code > 599 {
		return true
	}
	if gatewaystore.IsAlwaysSkipRetryCode(openaiErr.GetErrorCode()) {
		return false
	}
	return gatewaystore.ShouldRetryByStatusCode(code)
}

// withinGPTRetryFailureWindow only permits a GPT request to change upstreams
// shortly after it starts. A healthy but slow connection is never interrupted;
// the window is consulted only after an upstream error has already occurred.
func withinGPTRetryFailureWindow(c *gin.Context) bool {
	if c == nil || !strings.HasPrefix(strings.ToLower(c.GetString("original_model")), "gpt-") {
		return true
	}
	startTime := httpctx.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
	if startTime.IsZero() {
		return true
	}
	return time.Since(startTime) <= relaycommon.GPTInitialFailureRetryWindow
}

func canRetryResponsesStreamBeforeContent(c *gin.Context) bool {
	return gatewaystream.CanRetryResponsesBeforeSemanticOutput(c)
}

func finalizeRelayError(c *gin.Context, relayFormat types.RelayFormat, ws *websocket.Conn, apiErr *types.NewAPIError, requestID string) {
	if apiErr == nil {
		return
	}
	if c != nil && c.GetBool(string(constant.ContextKeyClientGone)) {
		return
	}
	recordFinalRelayFailureLog(c, apiErr)
	logger.LogError(c, fmt.Sprintf("relay error: %s", platformtext.LocalLogPreview(apiErr.Error())))
	if httpctx.GetContextKeyBool(c, constant.ContextKeyResponseBodyDelivered) {
		if !httpctx.GetContextKeyBool(c, constant.ContextKeyIsStream) || c.GetBool(string(constant.ContextKeyClientGone)) {
			return
		}
		apiErr.SanitizeDownstreamResponse()
		rawMessageWithRequestID := platformtext.MessageWithRequestID(apiErr.Error(), requestID)
		if types.IsRemoteProviderError(apiErr) {
			rawMessageWithRequestID = platformtext.SanitizeUpstreamProviderErrorMessage(rawMessageWithRequestID)
		}
		apiErr.SetMessage(rawMessageWithRequestID)
		if c.GetBool(string(constant.ContextKeyResponsesTerminalSent)) {
			return
		}
		switch relayFormat {
		case types.RelayFormatOpenAIRealtime:
			relaycommon.WssError(c, ws, apiErr.ToOpenAIError())
		case types.RelayFormatClaude:
			if err := gatewaystream.WriteClaudeStreamError(c, apiErr.ToClaudeError()); err != nil {
				logger.LogError(c, "write claude stream error failed: "+err.Error())
			}
		default:
			responses := c.Request != nil && strings.HasPrefix(c.Request.URL.Path, "/v1/responses")
			if err := gatewaystream.WriteOpenAIStreamError(c, apiErr.ToOpenAIError(), responses); err != nil {
				logger.LogError(c, "write openai stream error failed: "+err.Error())
			}
		}
		return
	}
	apiErr.SanitizeDownstreamResponse()
	rawMessageWithRequestID := platformtext.MessageWithRequestID(apiErr.Error(), requestID)
	if types.IsRemoteProviderError(apiErr) {
		rawMessageWithRequestID = platformtext.SanitizeUpstreamProviderErrorMessage(rawMessageWithRequestID)
	}
	apiErr.SetMessage(rawMessageWithRequestID)
	switch relayFormat {
	case types.RelayFormatOpenAIRealtime:
		relaycommon.WssError(c, ws, apiErr.ToOpenAIError())
	case types.RelayFormatClaude:
		c.JSON(apiErr.StatusCode, gin.H{
			"type":  "error",
			"error": apiErr.ToClaudeError(),
		})
	default:
		c.JSON(apiErr.StatusCode, gin.H{
			"error": apiErr.ToOpenAIError(),
		})
	}
}

// recordFinalRelayFailureLog makes terminal request failures visible in the
// user's usage log. Retried attempts that later succeed never reach this point,
// so each client request creates at most one zero-cost failure record.
func recordFinalRelayFailureLog(c *gin.Context, apiErr *types.NewAPIError) {
	if !shouldRecordFinalRelayFailureLog(c, apiErr) {
		return
	}

	startTime := httpctx.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
	useTimeSeconds := 0
	if !startTime.IsZero() {
		useTimeSeconds = int(time.Since(startTime).Seconds())
	}
	channelID := c.GetInt("channel_id")
	other := map[string]interface{}{
		"status":      "failed",
		"status_code": apiErr.StatusCode,
		"error_type":  apiErr.GetErrorType(),
		"error_code":  apiErr.GetErrorCode(),
		"retry_count": max(len(c.GetStringSlice("use_channel"))-1, 0),
	}
	if c.Request != nil && c.Request.URL != nil {
		other["request_path"] = c.Request.URL.Path
	}

	auditapp.RecordErrorLog(
		c,
		c.GetInt("id"),
		channelID,
		c.GetString("original_model"),
		c.GetString("token_name"),
		apiErr.MaskSensitiveErrorWithStatusCode(),
		c.GetInt("token_id"),
		useTimeSeconds,
		httpctx.GetContextKeyBool(c, constant.ContextKeyIsStream),
		c.GetString("group"),
		other,
	)
}

func shouldRecordFinalRelayFailureLog(c *gin.Context, apiErr *types.NewAPIError) bool {
	return c != nil && apiErr != nil && c.GetInt("id") > 0 && types.IsRecordErrorLog(apiErr)
}

func refundRelayBillingIfNeeded(c *gin.Context, relayInfo *relaycommon.RelayInfo, apiErr *types.NewAPIError) *types.NewAPIError {
	if apiErr == nil || relayInfo == nil {
		return apiErr
	}
	apiErr = billingapp.NormalizeViolationFeeError(apiErr)
	if relayInfo.Billing != nil {
		relayInfo.Billing.Refund(c)
	}
	billingapp.ChargeViolationFeeIfNeeded(c, relayInfo, apiErr)
	return apiErr
}

func recordRelayFailure(relayInfo *relaycommon.RelayInfo) {
	if relayInfo == nil {
		return
	}
	gopool.Go(func() {
		auditprojection.RecordRelaySample(relayInfo, false, 0)
	})
}

func shouldRecordRelayFailureSample(upstreamStarted bool, apiErr *types.NewAPIError) bool {
	return upstreamStarted && apiErr != nil
}

func restoreRelayRequestBody(c *gin.Context) *types.NewAPIError {
	bodyStorage, bodyErr := platformhttpx.GetBodyStorage(c)
	if bodyErr != nil {
		if platformhttpx.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, platformhttpx.ErrRequestBodyTooLarge) {
			return types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
		}
		return types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	c.Request.Body = io.NopCloser(bodyStorage)
	return nil
}
