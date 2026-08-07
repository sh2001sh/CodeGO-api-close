package http

import (
	"errors"
	"fmt"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/sh2001sh/new-api/constant"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	gatewayexecutionapp "github.com/sh2001sh/new-api/internal/gateway/execution/app"
	gatewayroutingapp "github.com/sh2001sh/new-api/internal/gateway/routing/app"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	identityapp "github.com/sh2001sh/new-api/internal/identity/app"
	platformconcurrency "github.com/sh2001sh/new-api/internal/platform/concurrency"
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

	releaseRelaySlot, admitted, admissionStats := platformconcurrency.TryAcquireRelaySlot()
	if !admitted {
		if admissionStats.Rejected == 1 || admissionStats.Rejected%100 == 0 {
			logger.LogWarn(c, fmt.Sprintf("relay admission rejected: active=%d capacity=%d rejected=%d", admissionStats.Active, admissionStats.Capacity, admissionStats.Rejected))
		}
		c.Header("Retry-After", "1")
		newAPIError = types.NewErrorWithStatusCode(
			errors.New(types.ServiceBusyMessage),
			types.ErrorCodeServiceBusy,
			http.StatusServiceUnavailable,
			types.ErrOptionWithSkipRetry(),
		)
		return
	}
	defer releaseRelaySlot()

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

	needSensitiveCheck := requestsettings.ShouldCheckPromptSensitive()
	needCountToken := constant.CountToken

	var meta *types.TokenCountMeta
	if needSensitiveCheck || needCountToken {
		meta = request.GetTokenCountMeta()
	} else {
		meta = fastTokenCountMetaForPricing(request)
	}

	if needSensitiveCheck && meta != nil {
		contains, words := identityapp.CheckSensitiveText(meta.CombineText)
		if contains {
			logger.LogWarn(c, fmt.Sprintf("user sensitive words detected: %s", strings.Join(words, ", ")))
			newAPIError = types.NewError(err, types.ErrorCodeSensitiveWordsDetected)
			return
		}
	}

	tokens, err := tokenx.EstimateRequestToken(c, meta, relayInfo)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeCountTokenFailed)
		return
	}
	relayInfo.SetEstimatePromptTokens(tokens)
	relaycommon.MarkLongContextRequestWithContinuation(c, relayInfo.OriginModelName, tokens, relaycommon.IsResponsesConversationRequest(request))

	priceData, err := relaycommon.ModelPriceHelper(c, relayInfo, tokens, meta)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest))
		return
	}
	if priceData.FreeModel {
		logger.LogInfo(c, fmt.Sprintf("妯″瀷 %s 鍏嶈垂锛岃烦杩囬鎵ｈ垂", relayInfo.OriginModelName))
	}

	retryTimes := gatewayroutingapp.EffectiveRetryTimes(relayInfo.TokenGroup)
	retryParam := &gatewayroutingapp.RetryParam{
		Ctx:        c,
		TokenGroup: relayInfo.TokenGroup,
		ModelName:  relayInfo.OriginModelName,
		Retry:      platformruntime.GetPointer(0),
	}
	relayInfo.RetryIndex = 0
	relayInfo.LastError = nil
	streamFailureCircuitChecked := false

	for ; retryParam.GetRetry() <= retryTimes; retryParam.IncreaseRetry() {
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
			if _, blocked := relaycommon.UserStreamFailureRetryAfter(c, relayInfo.OriginModelName); blocked {
				// A repeated incomplete stream is a signal to leave the currently
				// selected route, not a reason to reject the next Codex turn. No
				// semantic output has been sent for this request, so a bounded
				// cross-domain retry is safe.
				relaycommon.InvalidateChannelAffinityForCurrentRequest(c)
				addUsedChannel(c, channel.Id)
				relaycommon.ExcludeFaultDomain(c, c.GetString("channel_fault_domain"))
				relaycommon.ExcludeRouteDecisionCandidate(c, "user_stream_circuit")
				if retryTimes-retryParam.GetRetry() > 0 {
					relaycommon.RecordRouteDecisionRetry(c)
					gatewayroutingapp.RecordAutoGroupFailure(c, relayInfo.OriginModelName)
					continue
				}
			}
		}

		currentPriceData, priceErr := relaycommon.ModelPriceHelper(c, relayInfo, tokens, meta)
		if priceErr != nil {
			newAPIError = types.NewError(priceErr, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest))
			break
		}

		faultDomain := c.GetString("channel_fault_domain")
		releaseFaultDomainSlot, admitted, _ := relaycommon.TryAcquireFaultDomainSlot(faultDomain, relayInfo.OriginModelName)
		if !admitted {
			relaycommon.ExcludeFaultDomain(c, faultDomain)
			relaycommon.ExcludeRouteDecisionCandidate(c, "fault_domain_capacity")
			newAPIError = types.NewErrorWithStatusCode(
				errors.New("upstream fault domain is at capacity"),
				types.ErrorCodeGetChannelFailed,
				http.StatusServiceUnavailable,
			)
			if retryTimes-retryParam.GetRetry() > 0 {
				relaycommon.RecordRouteDecisionRetry(c)
				gatewayroutingapp.RecordAutoGroupFailure(c, relayInfo.OriginModelName)
				continue
			}
			break
		}

		addUsedChannel(c, channel.Id)
		if bodyErr := restoreRelayRequestBody(c); bodyErr != nil {
			releaseFaultDomainSlot(true, 0)
			newAPIError = bodyErr
			break
		}

		if !currentPriceData.FreeModel {
			newAPIError = billingapp.PreConsumeRelayBilling(c, currentPriceData.QuotaToPreConsume, relayInfo)
			if newAPIError != nil {
				releaseFaultDomainSlot(true, 0)
				break
			}
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
		if releaseFaultDomainSlot != nil {
			if newAPIError == nil || relaycommon.IsLocalStreamMaxDurationExceeded(c) {
				releaseFaultDomainSlot(true, 0)
			} else {
				releaseFaultDomainSlot(false, newAPIError.StatusCode)
			}
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
			relaycommon.RecordAIHubHealthSuccess(c, channel.Id, relayInfo.OriginModelName, ttft)
			relayInfo.LastError = nil
			return
		}

		newAPIError = billingapp.NormalizeViolationFeeError(newAPIError)
		relayInfo.LastError = newAPIError
		gatewayexecutionapp.ProcessChannelError(c,
			*types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, httpctx.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()),
			newAPIError,
		)

		if !shouldRetry(c, newAPIError, retryTimes-retryParam.GetRetry()) {
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
