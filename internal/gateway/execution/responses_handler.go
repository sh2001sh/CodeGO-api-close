package execution

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	appconstant "github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	gatewayproviders "github.com/sh2001sh/new-api/internal/gateway/execution/providers"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	platformcopy "github.com/sh2001sh/new-api/internal/platform/copyx"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	"github.com/sh2001sh/new-api/types"
	"io"
	"net/http"
	"strings"
)

func ResponsesHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *types.NewAPIError) {
	info.InitChannelMeta(c)
	if info.RelayMode == gatewaycontract.RelayModeResponsesCompact {
		switch info.ApiType {
		case appconstant.APITypeOpenAI, appconstant.APITypeCodex:
		default:
			return types.NewErrorWithStatusCode(
				fmt.Errorf("unsupported endpoint %q for api type %d", "/v1/responses/compact", info.ApiType),
				types.ErrorCodeInvalidRequest,
				http.StatusBadRequest,
				types.ErrOptionWithSkipRetry(),
			)
		}
	}

	var responsesReq *dto.OpenAIResponsesRequest
	switch req := info.Request.(type) {
	case *dto.OpenAIResponsesRequest:
		responsesReq = req
	case *dto.OpenAIResponsesCompactionRequest:
		responsesReq = &dto.OpenAIResponsesRequest{
			Model:              req.Model,
			Input:              req.Input,
			Instructions:       req.Instructions,
			Tools:              req.Tools,
			ParallelToolCalls:  normalizedCompactionParallelToolCalls(req.ParallelToolCalls),
			Reasoning:          req.Reasoning,
			ServiceTier:        req.ServiceTier,
			PromptCacheKey:     req.PromptCacheKey,
			Text:               req.Text,
			PreviousResponseID: req.PreviousResponseID,
		}
	default:
		return types.NewErrorWithStatusCode(
			fmt.Errorf("invalid request type, expected dto.OpenAIResponsesRequest or dto.OpenAIResponsesCompactionRequest, got %T", info.Request),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}

	// Routing and native Responses pass-through only replace top-level fields.
	// Defer the deep copy until conversion or a protocol bridge needs an
	// isolated mutable request; large input histories otherwise get duplicated
	// before they are sent unchanged upstream.
	requestCopy := *responsesReq
	request := &requestCopy
	var err error
	if err := relaycommon.ModelMappedHelper(c, info, request); err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}
	if info.ChannelMeta != nil && info.RelayMode == gatewaycontract.RelayModeResponsesCompact && !info.ResponsesCapabilities.AllowsRemoteCompactionV1For(request.Model, info.ChannelMultiKeyIndex) {
		return types.NewErrorWithStatusCode(fmt.Errorf("remote Responses compaction v1 is unsupported by channel %d", info.ChannelId), types.ErrorCodeDoRequestFailed, http.StatusServiceUnavailable)
	}
	if info.ChannelMeta != nil && info.RelayMode == gatewaycontract.RelayModeResponses && gatewaycontract.HasRemoteCompactionV2(c.Request.Header) && hasRemoteCompactionTrigger(responsesReq.Input) && !info.ResponsesCapabilities.AllowsRemoteCompactionV2For(request.Model, info.ChannelMultiKeyIndex) {
		return types.NewErrorWithStatusCode(fmt.Errorf("remote Responses compaction v2 is unsupported by channel %d", info.ChannelId), types.ErrorCodeDoRequestFailed, http.StatusServiceUnavailable)
	}

	adaptor := NewSyncAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)
	passThroughGlobal := gatewaystore.GetGlobalSettings().PassThroughRequestEnabled
	originalBodyFastPath := false
	var originalBody []byte
	if info.RelayMode == gatewaycontract.RelayModeResponses && !passThroughGlobal && !info.ChannelSetting.PassThroughBodyEnabled {
		if body, ok, err := tryResponsesOriginalBodyFastPath(c, info); err != nil {
			return types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
		} else if ok {
			originalBodyFastPath = true
			originalBody = body
		}
	}
	if info.RelayMode == gatewaycontract.RelayModeResponses &&
		!passThroughGlobal &&
		!info.ChannelSetting.PassThroughBodyEnabled &&
		shouldBridgeBeforeNative(info, bridgeResponsesToChat) {
		request, err = platformcopy.DeepCopy(request)
		if err != nil {
			return types.NewError(fmt.Errorf("failed to copy request to OpenAIResponsesRequest: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
		}
		return executeResponsesToChatBridge(c, info, adaptor, request)
	}

	var requestBody io.Reader
	var outboundJSON []byte
	preserveRemoteCompactionV2Body := false
	nativeRemoteCompactionV2 := info.RelayMode == gatewaycontract.RelayModeResponses && gatewaycontract.HasRemoteCompactionV2(c.Request.Header) && hasRemoteCompactionTrigger(responsesReq.Input)
	if nativeRemoteCompactionV2 {
		body, size, err := buildRemoteCompactionV2Body(c, responsesReq.Model, request.Model)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		info.UpstreamRequestBodySize = size
		outboundJSON, err = io.ReadAll(body)
		if err != nil {
			return types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
		}
		requestBody = bytes.NewReader(outboundJSON)
		preserveRemoteCompactionV2Body = true
	} else if originalBodyFastPath {
		outboundJSON = originalBody
		if info.FirstByteTrace != nil {
			info.FirstByteTrace.MarkRequestBodyFastPath()
		}
	} else if passThroughGlobal || info.ChannelSetting.PassThroughBodyEnabled {
		storage, err := platformhttpx.GetBodyStorage(c)
		if err != nil {
			return types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
		}
		jsonData, fastPath, err := forceResponsesStreamBody(storage)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		outboundJSON = jsonData
		if fastPath && info.FirstByteTrace != nil {
			info.FirstByteTrace.MarkRequestBodyFastPath()
		}
	} else {
		request, err = platformcopy.DeepCopy(request)
		if err != nil {
			return types.NewError(fmt.Errorf("failed to copy request to OpenAIResponsesRequest: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
		}
		convertedRequest, err := adaptor.ConvertOpenAIResponsesRequest(c, info, *request)
		if err != nil {
			if info.RelayMode == gatewaycontract.RelayModeResponses &&
				shouldFallbackAfterConversion(info, bridgeResponsesToChat, err) {
				bridgeError := executeResponsesToChatBridge(c, info, adaptor, request)
				if bridgeError == nil {
					rememberProtocolFallback(info, bridgeResponsesToChat)
				}
				return bridgeError
			}
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)
		jsonData, err := platformencoding.Marshal(convertedRequest)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}

		jsonData, err = relaycommon.RemoveDisabledFields(jsonData, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		if len(info.ParamOverride) > 0 {
			jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
			if err != nil {
				return newAPIErrorFromParamOverride(err)
			}
		}

		outboundJSON = jsonData
	}
	if len(outboundJSON) > 0 {
		if info.RelayMode == gatewaycontract.RelayModeResponsesCompact {
			normalized, changed, err := normalizeRemoteCompactionV1Body(outboundJSON)
			if err != nil {
				return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			}
			if changed {
				outboundJSON = normalized
			}
		}
		if !preserveRemoteCompactionV2Body && shouldNormalizeResponsesCompatibilityBody(outboundJSON) {
			normalized, changed, err := normalizeResponsesCompatibilityBody(outboundJSON)
			if err != nil {
				return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			}
			if changed {
				outboundJSON = normalized
			}
		}
		logger.LogDebug(c, "requestBody: %s", outboundJSON)
		requestBody = bytes.NewReader(outboundJSON)
	}
	if info.FirstByteTrace != nil {
		info.FirstByteTrace.MarkRequestConversionDone()
	}

	statusCodeMappingStr := c.GetString("status_code_mapping")
	httpResp, newAPIError := sendResponsesWithCompatibility(c, info, adaptor, requestBody, outboundJSON)
	if newAPIError != nil {
		if info.RelayMode == gatewaycontract.RelayModeResponses &&
			shouldFallbackAfterStatus(info, bridgeResponsesToChat, newAPIError) {
			bridgeError := executeResponsesToChatBridge(c, info, adaptor, request)
			if bridgeError == nil {
				rememberProtocolFallback(info, bridgeResponsesToChat)
				return nil
			}
			return preferBridgeError(newAPIError, bridgeError)
		}
		platformhttpx.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}

	usage, newAPIError := adaptor.DoResponse(c, httpResp, info)
	if newAPIError != nil {
		platformhttpx.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}

	usageDTO := usage.(*dto.Usage)
	if info.RelayMode == gatewaycontract.RelayModeResponsesCompact {
		originModelName := info.OriginModelName
		originPriceData := info.PriceData

		_, err := relaycommon.ModelPriceHelper(c, info, info.GetEstimatePromptTokens(), &types.TokenCountMeta{})
		if err != nil {
			info.OriginModelName = originModelName
			info.PriceData = originPriceData
			return types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithSkipRetry(), types.ErrOptionWithStatusCode(http.StatusBadRequest))
		}
		billingapp.PostTextConsumeQuota(c, info, usageDTO, nil)

		info.OriginModelName = originModelName
		info.PriceData = originPriceData
		return nil
	}

	if strings.HasPrefix(info.OriginModelName, "gpt-4o-audio") {
		billingapp.PostAudioConsumeQuota(c, info, usageDTO, "")
	} else {
		billingapp.PostTextConsumeQuota(c, info, usageDTO, nil)
	}
	return nil
}

func normalizedCompactionParallelToolCalls(raw json.RawMessage) json.RawMessage {
	// Codex's canonical CompactionInput serializes this required boolean even
	// when the client omits it. Supplying explicit false keeps v1 compaction
	// requests schema-complete and avoids provider-specific defaults changing
	// compaction semantics.
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage("false")
	}
	return raw
}

// tryResponsesOriginalBodyFastPath returns the already-decoded request body
// when the native Responses request can be sent byte-for-byte unchanged.
// Every condition here is intentionally conservative: if a model, protocol,
// field, or compatibility rewrite may be required, the normal conversion path
// remains in use.
func tryResponsesOriginalBodyFastPath(c *gin.Context, info *relaycommon.RelayInfo) ([]byte, bool, error) {
	if c == nil || c.Request == nil || info == nil || info.ChannelMeta == nil || info.RelayMode != gatewaycontract.RelayModeResponses || info.IsModelMapped || len(info.ParamOverride) > 0 {
		return nil, false, nil
	}
	if shouldBridgeBeforeNative(info, bridgeResponsesToChat) {
		return nil, false, nil
	}
	if info.OriginModelName == "" || (info.UpstreamModelName != "" && info.UpstreamModelName != info.OriginModelName) {
		return nil, false, nil
	}
	snapshot, err := platformhttpx.GetRequestBodySnapshot(c)
	if err != nil {
		return nil, false, err
	}
	if snapshot == nil || snapshot.Stream == nil || !*snapshot.Stream || snapshot.Model == "" {
		return nil, false, nil
	}
	if snapshot.Model != info.OriginModelName && (info.UpstreamModelName == "" || snapshot.Model != info.UpstreamModelName) {
		return nil, false, nil
	}
	body := snapshot.Raw
	if len(body) == 0 {
		storage, err := platformhttpx.GetBodyStorage(c)
		if err != nil {
			return nil, false, err
		}
		body, err = storage.Bytes()
		if err != nil {
			return nil, false, err
		}
	}
	if gatewaycontract.HasRemoteCompactionV2(c.Request.Header) && hasRemoteCompactionTrigger(json.RawMessage(body)) {
		return nil, false, nil
	}
	if shouldNormalizeResponsesCompatibilityBody(body) || requestBodyContainsDisabledFields(body, info.ChannelOtherSettings) {
		return nil, false, nil
	}
	return body, true, nil
}

func sendResponsesWithCompatibility(c *gin.Context, info *relaycommon.RelayInfo, adaptor gatewayproviders.SyncAdaptor, requestBody io.Reader, jsonBody []byte) (*http.Response, *types.NewAPIError) {
	resp, err := doResponsesRequest(c, info, adaptor, requestBody, jsonBody)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	httpResp, _ := resp.(*http.Response)
	if httpResp == nil || httpResp.StatusCode == http.StatusOK {
		return httpResp, nil
	}
	apiErr := platformhttpx.RelayErrorHandler(c.Request.Context(), httpResp, false)
	if retryJSON, ok := normalizePreviousResponseIDRetry(jsonBody, apiErr); ok {
		logger.LogInfo(c, "retrying Responses request without stale previous_response_id")
		resp, err = doResponsesRequest(c, info, adaptor, bytes.NewReader(retryJSON), retryJSON)
		if err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
		}
		httpResp, _ = resp.(*http.Response)
		if httpResp == nil || httpResp.StatusCode == http.StatusOK {
			return httpResp, nil
		}
		return nil, platformhttpx.RelayErrorHandler(c.Request.Context(), httpResp, false)
	}
	retryJSON, field, ok := normalizeRejectedResponsesField(jsonBody, apiErr)
	if !ok {
		return nil, apiErr
	}
	logger.LogInfo(c, fmt.Sprintf("retrying Responses request without rejected field %s", field))
	resp, err = doResponsesRequest(c, info, adaptor, bytes.NewReader(retryJSON), retryJSON)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	httpResp, _ = resp.(*http.Response)
	if httpResp == nil || httpResp.StatusCode == http.StatusOK {
		return httpResp, nil
	}
	return nil, platformhttpx.RelayErrorHandler(c.Request.Context(), httpResp, false)
}

func normalizePreviousResponseIDRetry(jsonBody []byte, apiErr *types.NewAPIError) ([]byte, bool) {
	if apiErr == nil || len(jsonBody) == 0 {
		return nil, false
	}
	oai := apiErr.ToOpenAIError()
	values := []string{fmt.Sprint(oai.Code), oai.Type, oai.Message}
	matched := false
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if strings.Contains(value, "previous_response_not_found") || strings.Contains(value, "previous response not found") || strings.Contains(value, "previous response is not available") {
			matched = true
			break
		}
	}
	if !matched || !bytes.Contains(jsonBody, []byte(`"previous_response_id"`)) {
		return nil, false
	}
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(jsonBody))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, false
	}
	if _, exists := payload["previous_response_id"]; !exists {
		return nil, false
	}
	delete(payload, "previous_response_id")
	retry, err := json.Marshal(payload)
	if err != nil {
		return nil, false
	}
	return retry, true
}

func doResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, adaptor gatewayproviders.SyncAdaptor, requestBody io.Reader, jsonBody []byte) (any, error) {
	if len(jsonBody) == 0 {
		return adaptor.DoRequest(c, info, requestBody)
	}
	// bytes.Reader gives net/http a replayable GetBody and ContentLength. Avoid
	// wrapping an already-materialized JSON body in a second BodyStorage, which
	// otherwise adds an extra allocation and file/memory lifecycle per attempt.
	info.UpstreamRequestBodySize = int64(len(jsonBody))
	return adaptor.DoRequest(c, info, bytes.NewReader(jsonBody))
}

func forceResponsesStreamBody(storage platformhttpx.BodyStorage) ([]byte, bool, error) {
	requestBody, err := storage.Bytes()
	if err != nil {
		return nil, false, err
	}
	var envelope struct {
		Stream *bool `json:"stream"`
	}
	if err := platformencoding.Unmarshal(requestBody, &envelope); err != nil {
		return nil, false, err
	}
	if envelope.Stream != nil && *envelope.Stream {
		return requestBody, true, nil
	}
	var body map[string]interface{}
	if err := platformencoding.Unmarshal(requestBody, &body); err != nil {
		return nil, false, err
	}
	body["stream"] = true
	encoded, err := platformencoding.Marshal(body)
	return encoded, false, err
}
