package http

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	gatewayexecutionapp "github.com/sh2001sh/new-api/internal/gateway/execution/app"
	responsesws "github.com/sh2001sh/new-api/internal/gateway/responsesws"
	routepin "github.com/sh2001sh/new-api/internal/gateway/routepin"
	gatewayroutingapp "github.com/sh2001sh/new-api/internal/gateway/routing/app"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystream "github.com/sh2001sh/new-api/internal/gateway/stream"
	identityapp "github.com/sh2001sh/new-api/internal/identity/app"
	marketplaceapp "github.com/sh2001sh/new-api/internal/marketplace/app"
	platformconcurrency "github.com/sh2001sh/new-api/internal/platform/concurrency"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	requestsettings "github.com/sh2001sh/new-api/internal/platform/requestsettings"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/sh2001sh/new-api/internal/platform/tokenx"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/sh2001sh/new-api/types"
)

func relayRequest(c *gin.Context, relayFormat types.RelayFormat) {
	requestID := c.GetString(constant.RequestIdKey)
	firstByteTrace := relaycommon.NewFirstByteTrace(httpctx.GetContextKeyTime(c, constant.ContextKeyRequestStartTime))

	var (
		newAPIError     *types.NewAPIError
		relayInfo       *relaycommon.RelayInfo
		upstreamStarted bool
	)

	ws, err := upgradeRelayWebsocket(c, relayFormat)
	if err != nil {
		return
	}
	if ws != nil {
		defer ws.Close()
	}

	defer func() {
		gatewayroutingapp.EndAutoGroupAttempt(c)
		relaycommon.ReleaseAllCoolingFallbacks(c)
		attachFinalRelayTiming(c, firstByteTrace)
		newAPIError = refundRelayBillingIfNeeded(c, relayInfo, newAPIError)
		if shouldRecordRelayFailureSample(upstreamStarted, newAPIError) {
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
	firstByteTrace.MarkRequestValidated()

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
	firstByteTrace.MarkAdmitted()

	relayInfo, err = relaycommon.GenRelayInfo(c, relayFormat, request, ws)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeGenRelayInfoFailed)
		return
	}
	relayInfo.FirstByteTrace = firstByteTrace
	if err := bindResponsesWebsocketRoute(c); err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
		return
	}
	firstByteTrace.MarkRelayInfoReady()
	if httpctx.GetContextKeyBool(c, constant.ContextKeyZeroHourActive) || httpctx.GetContextKeyBool(c, constant.ContextKeyMonthlyPassActive) {
		if specialMultiplierUnsupportedRelay(relayFormat, relayInfo.OriginModelName) {
			newAPIError = types.NewErrorWithStatusCode(errors.New("倍率卡分组仅支持文本和代码模型"), types.ErrorCodeAccessDenied, http.StatusForbidden, types.ErrOptionWithSkipRetry())
			return
		}
		releaseSlot, slotErr := acquireMultiplierCardSlot(c)
		if slotErr != nil {
			newAPIError = types.NewErrorWithStatusCode(slotErr, types.ErrorCodeAccessDenied, http.StatusTooManyRequests, types.ErrOptionWithSkipRetry())
			return
		}
		defer releaseSlot()
	}

	needSensitiveCheck := shouldCheckPromptSensitiveForRelay(relayFormat)
	needCountToken := constant.CountToken

	var meta *types.TokenCountMeta
	if needSensitiveCheck || needCountToken {
		meta = request.GetTokenCountMeta()
	} else {
		meta = fastTokenCountMetaForPricing(request)
	}

	tokens, err := tokenx.EstimateRequestToken(c, meta, relayInfo)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeCountTokenFailed)
		return
	}
	relayInfo.SetEstimatePromptTokens(tokens)
	relaycommon.MarkLongContextRequestWithContinuation(c, relayInfo.OriginModelName, tokens, relaycommon.IsResponsesConversationRequest(request))
	requestProfile := relaycommon.RefineRequestProfile(c, relayFormat, request, tokens)
	requestBudget := relaycommon.StartRequestBudget(
		c,
		requestProfile,
		httpctx.GetContextKeyTime(c, constant.ContextKeyRequestStartTime),
	)

	priceData, err := relaycommon.ModelPriceHelper(c, relayInfo, tokens, meta)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest))
		return
	}
	if priceData.FreeModel {
		logger.LogInfo(c, fmt.Sprintf("妯″瀷 %s 鍏嶈垂锛岃烦杩囬鎵ｈ垂", relayInfo.OriginModelName))
	}

	retryTimes := gatewayroutingapp.EffectiveRetryTimes(relayInfo.TokenGroup)
	if bindings, found := httpctx.GetContextKeyType[[]marketplaceapp.RoutingBinding](c, constant.ContextKeyUnifiedAutoBindings); found {
		if remaining := len(bindings) - 1; remaining > retryTimes {
			retryTimes = remaining
		}
		relaycommon.ExpandRequestBudget(requestBudget, len(bindings))
	}
	if session := responsesws.FromContext(c); session != nil && session.NativeEnabled() {
		retryTimes = 1
		relaycommon.ExpandRequestBudget(requestBudget, 2)
	}
	if requestBudget != nil && retryTimes >= requestBudget.MaxAttempts {
		retryTimes = requestBudget.MaxAttempts - 1
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
	relayInfo.FirstByteTrace.MarkPreflightDone()

	for ; retryParam.GetRetry() <= retryTimes; retryParam.IncreaseRetry() {
		relayInfo.RetryIndex = retryParam.GetRetry()
		var (
			channel    *gatewayschema.Channel
			channelErr *types.NewAPIError
		)
		for selectionAttempt := 0; ; selectionAttempt++ {
			channel, channelErr = getChannel(c, relayInfo, retryParam)
			if channelErr == nil || !shouldWaitForRouteSelection(c, selectionAttempt) {
				break
			}
			relaycommon.RecordRouteDecisionRetry(c)
			if !waitForRouteSelection(c, selectionAttempt) {
				break
			}
		}
		if channelErr != nil {
			logger.LogError(c, channelErr.Error())
			newAPIError = channelErr
			break
		}
		relayInfo.FirstByteTrace.MarkRouteSelected()
		relayInfo.InitChannelMeta(c)
		if sensitiveErr := checkPromptSensitiveForChannel(c, relayFormat, channel, meta); sensitiveErr != nil {
			newAPIError = sensitiveErr
			break
		}
		if auditErr := checkPromptAudit(c, relayFormat, request, relayInfo); auditErr != nil {
			newAPIError = auditErr
			break
		}
		gatewaystream.BeginRelayAttempt(c)
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

		releaseChannelConcurrency, channelAdmitted := relaycommon.TryBeginChannelRequestForUser(
			channel.Id,
			relayInfo.UserId,
			channel.MarketplaceMaxConcurrency,
			channel.MarketplaceUserMaxConcurrency,
		)
		if !channelAdmitted {
			addUsedChannel(c, channel.Id)
			relaycommon.ExcludeRouteDecisionCandidate(c, "channel_capacity")
			// This is local backpressure, not an upstream failure. Do not feed it
			// into channel health or the auto-group circuit.
			newAPIError = types.NewErrorWithStatusCode(
				errors.New("channel concurrency limit reached"),
				types.ErrorCodeGetChannelFailed,
				http.StatusServiceUnavailable,
			)
			if retryTimes-retryParam.GetRetry() > 0 {
				relaycommon.RecordRouteDecisionRetry(c)
				continue
			}
			break
		}
		addUsedChannel(c, channel.Id)

		faultDomain := c.GetString("channel_fault_domain")
		capacityScope := httpctx.GetContextKeyString(c, constant.ContextKeyMarketplaceGroupID)
		if capacityScope == "" {
			capacityScope = httpctx.GetContextKeyString(c, constant.ContextKeyUsingGroup)
		}
		if capacityScope == "" {
			capacityScope = fmt.Sprintf("channel:%d", channel.Id)
		}
		releaseFaultDomainSlot, admitted, _ := relaycommon.TryAcquireFaultDomainSlotForUser(
			faultDomain,
			capacityScope,
			relayInfo.OriginModelName,
			relayInfo.UserId,
			requestProfile.RequestType,
		)
		if !admitted {
			relaycommon.ExcludeFaultDomain(c, faultDomain)
			relaycommon.ExcludeRouteDecisionCandidate(c, "fault_domain_capacity")
			releaseChannelConcurrency()
			// Fault-domain capacity is local backpressure. It is not a channel
			// failure and must not enter channel or auto-group cooldown accounting.
			newAPIError = types.NewErrorWithStatusCode(
				errors.New("upstream fault domain is at capacity"),
				types.ErrorCodeGetChannelFailed,
				http.StatusServiceUnavailable,
			)
			if retryTimes-retryParam.GetRetry() > 0 {
				relaycommon.RecordRouteDecisionRetry(c)
				continue
			}
			break
		}
		relayInfo.FirstByteTrace.MarkRequestBodyRestoreStarted()
		bodyErr := restoreRelayRequestBody(c)
		relayInfo.FirstByteTrace.MarkRequestBodyRestoreDone()
		if bodyErr != nil {
			releaseChannelConcurrency()
			releaseFaultDomainSlot(true, 0)
			newAPIError = bodyErr
			break
		}

		if !currentPriceData.FreeModel {
			relayInfo.FirstByteTrace.MarkBillingReserveStarted()
			newAPIError = billingapp.PreConsumeRelayBilling(c, currentPriceData.QuotaToPreConsume, relayInfo)
			relayInfo.FirstByteTrace.MarkBillingReserveDone()
			if newAPIError != nil {
				releaseChannelConcurrency()
				releaseFaultDomainSlot(true, 0)
				break
			}
		}
		if requestBudget != nil && !requestBudget.TryBeginAttempt(time.Now(), faultDomain) {
			releaseChannelConcurrency()
			releaseFaultDomainSlot(true, 0)
			relaycommon.ExcludeRouteDecisionCandidate(c, "request_budget_exhausted")
			newAPIError = types.NewErrorWithStatusCode(
				errors.New("upstream request budget exhausted"),
				types.ErrorCodeGetChannelFailed,
				http.StatusServiceUnavailable,
				types.ErrOptionWithSkipRetry(),
			)
			break
		}
		relaycommon.UpdateRouteDecisionBudget(c, requestBudget)
		relaycommon.StartRouteDecisionAttempt(c, relayInfo.RetryIndex, channel.Id, faultDomain)
		relayInfo.BeginAttempt(time.Now())
		relayInfo.FirstByteTrace.MarkUpstreamStart()
		upstreamStarted = true
		gatewayroutingapp.BeginAutoGroupAttempt(c, relayInfo.OriginModelName)

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
		gatewayroutingapp.EndAutoGroupAttempt(c)
		releaseChannelConcurrency()
		if releaseFaultDomainSlot != nil {
			if newAPIError == nil || relaycommon.IsLocalStreamMaxDurationExceeded(c) {
				releaseFaultDomainSlot(true, 0)
			} else {
				releaseFaultDomainSlot(false, newAPIError.StatusCode)
			}
		}
		// The all-cooling bulkhead protects upstream attempts, not the lifetime
		// of a downstream stream. Release before evaluating a retry so another
		// request can reselect after this attempt has updated route health.
		relaycommon.ReleaseAllCoolingFallbacks(c)

		if newAPIError == nil {
			relaycommon.FinishRouteDecisionAttempt(c, true, 0, "", string(gatewaystream.AttemptStageFromContext(c)))
			gatewayroutingapp.RecordAutoGroupSuccess(c, relayInfo.OriginModelName)
			if httpctx.GetContextKeyBool(c, constant.ContextKeyIsStream) {
				relaycommon.ClearUserStreamFailureCircuit(c, relayInfo.OriginModelName)
			}
			ttft := relayInfo.FirstResponseTime.Sub(relayInfo.StartTime)
			if !relayInfo.HasSendResponse() {
				ttft = 0
			}
			relaycommon.RecordChannelSuccess(channel.Id, relayInfo.OriginModelName, ttft, requestProfile.RequestType)
			relaycommon.RecordChannelCredentialSuccess(channel.Id)
			relaycommon.RecordFaultDomainSuccess(c.GetString("channel_fault_domain"), relayInfo.OriginModelName, requestProfile.RequestType)
			if relaycommon.IsAutoRouteRequest(c) {
				relaycommon.RecordUserChannelSuccess(c, channel.Id, relayInfo.OriginModelName, ttft, requestProfile.RequestType)
				relaycommon.RecordUserFaultDomainSuccess(c, c.GetString("channel_fault_domain"), relayInfo.OriginModelName, requestProfile.RequestType)
			}
			relayInfo.LastError = nil
			return
		}

		newAPIError = billingapp.NormalizeViolationFeeError(newAPIError)
		relayInfo.LastError = newAPIError
		gatewayexecutionapp.ProcessChannelError(c,
			*types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, httpctx.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()),
			newAPIError,
		)
		if shouldRecordAutoGroupFailure(c, newAPIError) {
			gatewayroutingapp.RecordAutoGroupFailure(c, relayInfo.OriginModelName)
		}
		if c.GetBool("responses_ephemeral_websocket") {
			if session := responsesws.FromContext(c); session != nil {
				session.ResetRoute()
			}
			routepin.Clear(c)
		}

		if !shouldRetry(c, newAPIError, retryTimes-retryParam.GetRetry()) {
			break
		}
		relaycommon.RecordRouteDecisionRetry(c)
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("retry channels: %s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}
}

func specialMultiplierUnsupportedRelay(relayFormat types.RelayFormat, modelName string) bool {
	if gatewaycontract.IsImageGenerationModel(modelName) {
		return true
	}
	switch relayFormat {
	case types.RelayFormatOpenAIImage, types.RelayFormatOpenAIAudio, types.RelayFormatOpenAIRealtime:
		return true
	default:
		return false
	}
}

func shouldBlockSensitiveWords() bool {
	return requestsettings.StopOnSensitiveEnabled
}

func shouldCheckPromptSensitiveForRelay(relayFormat types.RelayFormat) bool {
	// Claude requests commonly include provider safety/system/tool text. Do
	// not apply the site's prompt interception to that protocol, otherwise
	// harmless user prompts can match vocabulary embedded in metadata.
	return requestsettings.ShouldCheckPromptSensitive() && relayFormat != types.RelayFormatClaude
}

func checkPromptSensitiveForChannel(c *gin.Context, relayFormat types.RelayFormat, channel *gatewayschema.Channel, meta *types.TokenCountMeta) *types.NewAPIError {
	if !shouldCheckPromptSensitiveForRelay(relayFormat) || meta == nil {
		return nil
	}
	if !channel.ShouldInterceptSensitiveWords() {
		return nil
	}
	contains, words := identityapp.CheckSensitiveText(meta.CombineText)
	if !contains {
		return nil
	}
	logger.LogWarn(c, fmt.Sprintf("user sensitive words detected: %s", strings.Join(words, ", ")))
	if shouldBlockSensitiveWords() {
		return types.NewError(errors.New("sensitive words detected"), types.ErrorCodeSensitiveWordsDetected)
	}
	return nil
}
