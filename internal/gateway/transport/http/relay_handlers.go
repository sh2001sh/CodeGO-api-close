package http

import (
	"errors"
	"fmt"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/sh2001sh/new-api/constant"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	gatewayexecutionapp "github.com/sh2001sh/new-api/internal/gateway/execution/app"
	gatewayroutingapp "github.com/sh2001sh/new-api/internal/gateway/routing/app"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	identityapp "github.com/sh2001sh/new-api/internal/identity/app"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	requestsettings "github.com/sh2001sh/new-api/internal/platform/requestsettings"
	"github.com/sh2001sh/new-api/internal/platform/tokenx"
	"github.com/sh2001sh/new-api/internal/platform/transport/http/middleware"
	"github.com/sh2001sh/new-api/types"
)

// RelayWithFormat handles the main synchronous relay entrypoints for a specific protocol shape.
func RelayWithFormat(relayFormat types.RelayFormat) gin.HandlerFunc {
	return func(c *gin.Context) {
		relayRequest(c, relayFormat)
	}
}

// Playground handles authenticated playground text requests.
func Playground(c *gin.Context) {
	if c.GetBool("use_access_token") {
		respondRelayError(c, types.NewError(errors.New("暂不支持使用 access token"), types.ErrorCodeAccessDenied, types.ErrOptionWithSkipRetry()))
		return
	}

	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatOpenAI, nil, nil)
	if err != nil {
		respondRelayError(c, types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry()))
		return
	}

	userID := c.GetInt("id")
	if err := identityapp.WriteUserCacheToContext(c, userID); err != nil {
		respondRelayError(c, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry()))
		return
	}

	tempToken := &identityschema.Token{
		UserId: userID,
		Name:   fmt.Sprintf("playground-%s", relayInfo.UsingGroup),
		Group:  relayInfo.UsingGroup,
	}
	_ = middleware.SetupContextForToken(c, tempToken)

	relayRequest(c, types.RelayFormatOpenAI)
}

// PlaygroundImage handles authenticated image workspace relay requests.
func PlaygroundImage(c *gin.Context) {
	if c.GetBool("use_access_token") {
		respondRelayError(c, types.NewError(errors.New("暂不支持使用 access token"), types.ErrorCodeAccessDenied, types.ErrOptionWithSkipRetry()))
		return
	}

	meta, err := identityapp.BuildImageWorkspaceMetaFromRequest(c)
	if err != nil {
		respondRelayError(c, types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry()))
		return
	}

	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatOpenAIImage, nil, nil)
	if err != nil {
		respondRelayError(c, types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry()))
		return
	}

	userID := c.GetInt("id")
	if err := identityapp.WriteUserCacheToContext(c, userID); err != nil {
		respondRelayError(c, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry()))
		return
	}

	tempToken := &identityschema.Token{
		UserId: userID,
		Name:   fmt.Sprintf("image-workspace-%s", relayInfo.UsingGroup),
		Group:  relayInfo.UsingGroup,
	}
	_ = middleware.SetupContextForToken(c, tempToken)

	c.Set(string(constant.ContextKeyImageWorkspaceCaptureResponse), true)
	relayRequest(c, types.RelayFormatOpenAIImage)

	if c.Writer.Status() >= http.StatusBadRequest {
		return
	}
	rawResponse, ok := c.Get(string(constant.ContextKeyImageWorkspaceResponseBody))
	if !ok {
		return
	}
	responseBody, ok := rawResponse.([]byte)
	if !ok || len(responseBody) == 0 {
		return
	}
	_, _ = identityapp.PersistImageWorkspaceResponse(c, meta, responseBody)
}

// RelayNotImplemented returns a standard OpenAI-style "not implemented" response.
func RelayNotImplemented(c *gin.Context) {
	err := types.OpenAIError{
		Message: "API not implemented",
		Type:    "new_api_error",
		Param:   "",
		Code:    "api_not_implemented",
	}
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": err,
	})
}

// RelayNotFound returns a standard OpenAI-style "invalid URL" response.
func RelayNotFound(c *gin.Context) {
	err := types.OpenAIError{
		Message: fmt.Sprintf("Invalid URL (%s %s)", c.Request.Method, c.Request.URL.Path),
		Type:    "invalid_request_error",
		Param:   "",
		Code:    "",
	}
	c.JSON(http.StatusNotFound, gin.H{
		"error": err,
	})
}

func relayRequest(c *gin.Context, relayFormat types.RelayFormat) {
	requestID := c.GetString(constant.RequestIdKey)
	requestPreparationStartedAt := time.Now()

	var (
		newAPIError *types.NewAPIError
		relayInfo   *relaycommon.RelayInfo
	)

	ws, err := upgradeRelayWebsocket(c, relayFormat)
	if err != nil {
		return
	}
	if ws != nil {
		defer ws.Close()
	}

	defer func() {
		newAPIError = refundRelayBillingIfNeeded(c, relayInfo, newAPIError)
		if newAPIError != nil {
			recordRelayFailure(relayInfo)
		}
		finalizeRelayError(c, relayFormat, ws, newAPIError, requestID)
	}()

	request, err := getAndValidateRequest(c, relayFormat)
	if err != nil {
		if platformhttpx.IsRequestBodyTooLargeError(err) || errors.Is(err, platformhttpx.ErrRequestBodyTooLarge) {
			newAPIError = types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
		} else {
			newAPIError = types.NewError(err, types.ErrorCodeInvalidRequest)
		}
		return
	}

	relayInfo, err = relaycommon.GenRelayInfo(c, relayFormat, request, ws)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeGenRelayInfoFailed)
		return
	}
	if httpctx.GetContextKeyBool(c, constant.ContextKeyZeroHourActive) {
		if relayFormat == types.RelayFormatOpenAIImage || gatewaycontract.IsImageGenerationModel(relayInfo.OriginModelName) {
			newAPIError = types.NewErrorWithStatusCode(errors.New("0 倍率分组不支持生图模型"), types.ErrorCodeAccessDenied, http.StatusForbidden, types.ErrOptionWithSkipRetry())
			return
		}
		releaseSlot, slotErr := acquireZeroHourSlot(c)
		if slotErr != nil {
			newAPIError = types.NewErrorWithStatusCode(slotErr, types.ErrorCodeAccessDenied, http.StatusTooManyRequests, types.ErrOptionWithSkipRetry())
			return
		}
		defer releaseSlot()
	}
	preflightStartedAt := time.Now()
	requestPreparationDuration := preflightStartedAt.Sub(requestPreparationStartedAt)
	var tokenPreparationDuration time.Duration
	var billingPreparationDuration time.Duration
	preflightLogged := false

	needSensitiveCheck := requestsettings.ShouldCheckPromptSensitive()
	needPromptSafety := requestsettings.ShouldRunPromptSafety()
	needCountToken := constant.CountToken

	var meta *types.TokenCountMeta
	if needSensitiveCheck || needPromptSafety || needCountToken {
		meta = request.GetTokenCountMeta()
	} else {
		meta = fastTokenCountMetaForPricing(request)
	}

	if needSensitiveCheck && meta != nil {
		contains, words := identityapp.CheckSensitiveText(meta.CombineText)
		if contains {
			logger.LogWarn(c, fmt.Sprintf("user sensitive words detected: %s", strings.Join(words, ", ")))
			newAPIError = types.NewErrorWithStatusCode(errors.New("请求未通过内容安全检查"), types.ErrorCodeSensitiveWordsDetected, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
			return
		}
	}

	if needPromptSafety && meta != nil {
		decision := identityapp.EvaluatePromptSafety(c.GetInt("id"), meta.CombineText)
		if decision.LocalRisk > 0 {
			logger.LogWarn(c, fmt.Sprintf("prompt safety risk detected: user=%d score=%d labels=%s", c.GetInt("id"), decision.RiskScore, strings.Join(decision.Labels, ",")))
		}
		if decision.Block {
			newAPIError = types.NewErrorWithStatusCode(errors.New("请求未通过内容安全检查"), types.ErrorCodePromptBlocked, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
			return
		}
	}

	tokenPreparationStartedAt := time.Now()
	tokens, err := tokenx.EstimateRequestToken(c, meta, relayInfo)
	tokenPreparationDuration = time.Since(tokenPreparationStartedAt)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeCountTokenFailed)
		return
	}
	relayInfo.SetEstimatePromptTokens(tokens)
	relaycommon.MarkLongContextRequest(c, relayInfo.OriginModelName, tokens)

	priceData, err := relaycommon.ModelPriceHelper(c, relayInfo, tokens, meta)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest))
		return
	}
	if priceData.FreeModel {
		logger.LogInfo(c, fmt.Sprintf("妯″瀷 %s 鍏嶈垂锛岃烦杩囬鎵ｈ垂", relayInfo.OriginModelName))
	}

	retryParam := &gatewayroutingapp.RetryParam{
		Ctx:        c,
		TokenGroup: relayInfo.TokenGroup,
		ModelName:  relayInfo.OriginModelName,
		Retry:      platformruntime.GetPointer(0),
	}
	relayInfo.RetryIndex = 0
	relayInfo.LastError = nil
	streamFailureCircuitChecked := false

	for ; retryParam.GetRetry() <= platformconfig.RetryTimes; retryParam.IncreaseRetry() {
		relayInfo.RetryIndex = retryParam.GetRetry()
		channel, channelErr := getChannel(c, relayInfo, retryParam)
		if channelErr != nil {
			logger.LogError(c, channelErr.Error())
			newAPIError = channelErr
			break
		}
		relayInfo.InitChannelMeta(c)
		if httpctx.GetContextKeyBool(c, constant.ContextKeyIsStream) && !streamFailureCircuitChecked {
			streamFailureCircuitChecked = true
			if retryAfterSeconds, blocked := relaycommon.UserStreamFailureRetryAfter(c, relayInfo.OriginModelName); blocked {
				c.Header("Retry-After", strconv.Itoa(retryAfterSeconds))
				newAPIError = types.NewErrorWithStatusCode(
					errors.New(types.ModelUnavailableMessage),
					types.ErrorCodeGetChannelFailed,
					http.StatusServiceUnavailable,
					types.ErrOptionWithSkipRetry(),
				)
				break
			}
		}

		currentPriceData, priceErr := relaycommon.ModelPriceHelper(c, relayInfo, tokens, meta)
		if priceErr != nil {
			newAPIError = types.NewError(priceErr, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest))
			break
		}

		addUsedChannel(c, channel.Id)
		if bodyErr := restoreRelayRequestBody(c); bodyErr != nil {
			newAPIError = bodyErr
			break
		}

		if !currentPriceData.FreeModel {
			billingPreparationStartedAt := time.Now()
			newAPIError = billingapp.PreConsumeRelayBilling(c, currentPriceData.QuotaToPreConsume, relayInfo)
			billingPreparationDuration += time.Since(billingPreparationStartedAt)
			if newAPIError != nil {
				break
			}
		}
		if !preflightLogged {
			logSlowRelayPreflight(c, relayInfo, time.Since(preflightStartedAt), requestPreparationDuration, tokenPreparationDuration, billingPreparationDuration)
			preflightLogged = true
		}

		switch relayFormat {
		case types.RelayFormatOpenAIRealtime:
			newAPIError = gatewayexecutionapp.ExecuteRealtimeRelay(c, relayInfo)
		case types.RelayFormatClaude:
			newAPIError = gatewayexecutionapp.ExecuteClaudeRelay(c, relayInfo)
		case types.RelayFormatGemini:
			newAPIError = geminiRelayHandler(c, relayInfo)
		default:
			newAPIError = relayHandler(c, relayInfo)
		}

		if newAPIError == nil {
			gatewayroutingapp.RecordAutoGroupSuccess(c, relayInfo.OriginModelName)
			if httpctx.GetContextKeyBool(c, constant.ContextKeyIsStream) {
				relaycommon.ClearUserStreamFailureCircuit(c, relayInfo.OriginModelName)
			}
			ttft := relayInfo.FirstResponseTime.Sub(relayInfo.StartTime)
			if !relayInfo.HasSendResponse() {
				ttft = 0
			}
			relaycommon.RecordChannelSuccess(channel.Id, relayInfo.OriginModelName, ttft)
			relaycommon.RecordFaultDomainSuccess(c.GetString("channel_fault_domain"), relayInfo.OriginModelName)
			relayInfo.LastError = nil
			return
		}

		newAPIError = billingapp.NormalizeViolationFeeError(newAPIError)
		relayInfo.LastError = newAPIError
		gatewayexecutionapp.ProcessChannelError(c,
			*types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, httpctx.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()),
			newAPIError,
		)

		if !shouldRetry(c, newAPIError, platformconfig.RetryTimes-retryParam.GetRetry()) {
			break
		}
		relaycommon.RecordRouteDecisionRetry(c)
		gatewayroutingapp.RecordAutoGroupFailure(c, relayInfo.OriginModelName)
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("retry channels: %s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}
}

// logSlowRelayPreflight records only aggregate timing for requests that spend
// meaningful time in CodeGo before the first upstream attempt. It deliberately
// excludes request content and credentials.
func logSlowRelayPreflight(c *gin.Context, info *relaycommon.RelayInfo, total, requestPreparation, tokenPreparation, billingPreparation time.Duration) {
	const slowPreflightThreshold = 250 * time.Millisecond
	if total < slowPreflightThreshold || info == nil {
		return
	}

	logger.LogInfo(c, fmt.Sprintf(
		"relay preflight slow: request_id=%s model=%s channel=%d total_ms=%d request_ms=%d token_ms=%d billing_ms=%d",
		info.RequestId,
		info.OriginModelName,
		c.GetInt("channel_id"),
		total.Milliseconds(),
		requestPreparation.Milliseconds(),
		tokenPreparation.Milliseconds(),
		billingPreparation.Milliseconds(),
	))
}

func upgradeRelayWebsocket(c *gin.Context, relayFormat types.RelayFormat) (*websocket.Conn, error) {
	if relayFormat != types.RelayFormatOpenAIRealtime {
		return nil, nil
	}
	ws, err := relayUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		relaycommon.WssError(c, nil, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry()).ToOpenAIError())
		return nil, err
	}
	return ws, nil
}

func respondRelayError(c *gin.Context, newAPIError *types.NewAPIError) {
	if newAPIError == nil {
		return
	}
	newAPIError.SanitizeDownstreamResponse()
	c.JSON(newAPIError.StatusCode, gin.H{
		"error": newAPIError.ToOpenAIError(),
	})
}
