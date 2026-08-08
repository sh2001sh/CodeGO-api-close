package http

import (
	"errors"
	"fmt"
	platformtext "github.com/sh2001sh/new-api/internal/platform/textx"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/samber/lo"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	auditprojection "github.com/sh2001sh/new-api/internal/audit/projection"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	gatewayexecutionapp "github.com/sh2001sh/new-api/internal/gateway/execution/app"
	gatewayroutingapp "github.com/sh2001sh/new-api/internal/gateway/routing/app"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	gatewaystream "github.com/sh2001sh/new-api/internal/gateway/stream"
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

	c.Set(routeSelectionExhaustedContextKey, false)
	channel, selectGroup, err := gatewayroutingapp.CacheGetRandomSatisfiedChannel(retryParam)
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
