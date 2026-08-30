package execution

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformcopy "github.com/sh2001sh/new-api/internal/platform/copyx"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/sh2001sh/new-api/types"
	"io"
	"net/http"
	"strings"
)

func TextHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *types.NewAPIError) {
	info.InitChannelMeta(c)

	textReq, ok := info.Request.(*dto.GeneralOpenAIRequest)
	if !ok {
		return types.NewErrorWithStatusCode(fmt.Errorf("invalid request type, expected dto.GeneralOpenAIRequest, got %T", info.Request), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	// Keep the routing phase allocation-light. Model mapping and stream option
	// normalization only replace top-level fields; defer the deep copy until a
	// provider conversion is actually required so large message arrays are not
	// duplicated on native pass-through requests.
	requestCopy := *textReq
	request := &requestCopy
	var err error

	if request.WebSearchOptions != nil {
		c.Set("chat_completion_web_search_context_size", request.WebSearchOptions.SearchContextSize)
	}
	if err := relaycommon.ModelMappedHelper(c, info, request); err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}

	includeUsage := true
	if request.StreamOptions != nil {
		includeUsage = request.StreamOptions.IncludeUsage
	}
	if !info.SupportStreamOptions || !lo.FromPtrOr(request.Stream, false) {
		request.StreamOptions = nil
	} else if constant.ForceStreamOption {
		request.StreamOptions = &dto.StreamOptions{IncludeUsage: true}
	}
	info.ShouldIncludeUsage = includeUsage

	adaptor := NewSyncAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)

	passThroughGlobal := gatewaystore.GetGlobalSettings().PassThroughRequestEnabled
	if info.RelayMode == gatewaycontract.RelayModeChatCompletions &&
		!passThroughGlobal &&
		!info.ChannelSetting.PassThroughBodyEnabled &&
		shouldBridgeBeforeNative(info, bridgeChatToResponses) {
		request, err = platformcopy.DeepCopy(request)
		if err != nil {
			return types.NewError(fmt.Errorf("failed to copy request to GeneralOpenAIRequest: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
		}
		return executeChatToResponsesBridge(c, info, adaptor, request)
	}
	originalBodyFastPath := false
	if !passThroughGlobal && !info.ChannelSetting.PassThroughBodyEnabled {
		originalBodyFastPath, err = tryChatCompletionsOriginalBodyFastPath(c, info, textReq, request)
		if err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
	}

	var requestBody io.Reader
	if passThroughGlobal || info.ChannelSetting.PassThroughBodyEnabled {
		storage, err := platformhttpx.GetBodyStorage(c)
		if err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		if platformconfig.DebugEnabled {
			if debugBytes, bodyErr := storage.Bytes(); bodyErr == nil {
				println("requestBody: ", string(debugBytes))
			}
		}
		requestBody = platformhttpx.ReaderOnly(storage)
	} else if originalBodyFastPath {
		storage, storageErr := platformhttpx.GetBodyStorage(c)
		if storageErr != nil {
			return types.NewErrorWithStatusCode(storageErr, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		requestBody = platformhttpx.ReaderOnly(storage)
		info.UpstreamRequestBodySize = storage.Size()
		info.ReasoningEffort = request.ReasoningEffort
		if info.FirstByteTrace != nil {
			info.FirstByteTrace.MarkRequestBodyFastPath()
		}
	} else {
		request, err = platformcopy.DeepCopy(request)
		if err != nil {
			return types.NewError(fmt.Errorf("failed to copy request to GeneralOpenAIRequest: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
		}
		convertedRequest, err := adaptor.ConvertOpenAIRequest(c, info, request)
		if err != nil {
			if info.RelayMode == gatewaycontract.RelayModeChatCompletions &&
				shouldFallbackAfterConversion(info, bridgeChatToResponses, err) {
				bridgeError := executeChatToResponsesBridge(c, info, adaptor, request)
				if bridgeError == nil {
					rememberProtocolFallback(info, bridgeChatToResponses)
				}
				return bridgeError
			}
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)

		if info.ChannelSetting.SystemPrompt != "" {
			request, ok := convertedRequest.(*dto.GeneralOpenAIRequest)
			if ok {
				containSystemPrompt := false
				for _, message := range request.Messages {
					if message.Role == request.GetSystemRoleName() {
						containSystemPrompt = true
						break
					}
				}
				if !containSystemPrompt {
					request.Messages = append([]dto.Message{{
						Role:    request.GetSystemRoleName(),
						Content: info.ChannelSetting.SystemPrompt,
					}}, request.Messages...)
				} else if info.ChannelSetting.SystemPromptOverride {
					httpctx.SetContextKey(c, constant.ContextKeySystemPromptOverride, true)
					for i, message := range request.Messages {
						if message.Role != request.GetSystemRoleName() {
							continue
						}
						if message.IsStringContent() {
							request.Messages[i].SetStringContent(info.ChannelSetting.SystemPrompt + "\n" + message.StringContent())
						} else {
							contents := message.ParseContent()
							contents = append([]dto.MediaContent{{
								Type: dto.ContentTypeText,
								Text: info.ChannelSetting.SystemPrompt,
							}}, contents...)
							request.Messages[i].Content = contents
						}
						break
					}
				}
			}
		}

		jsonData, err := platformencoding.Marshal(convertedRequest)
		if err != nil {
			return types.NewError(err, types.ErrorCodeJsonMarshalFailed, types.ErrOptionWithSkipRetry())
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

		logger.LogDebug(c, fmt.Sprintf("text request body: %s", string(jsonData)))
		body, size, closer, err := relaycommon.NewOutboundJSONBody(jsonData)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		defer closer.Close()
		info.UpstreamRequestBodySize = size
		requestBody = body
	}

	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}

	statusCodeMappingStr := c.GetString("status_code_mapping")
	var httpResp *http.Response
	if resp != nil {
		httpResp = resp.(*http.Response)
		info.IsStream = info.IsStream || strings.HasPrefix(httpResp.Header.Get("Content-Type"), "text/event-stream")
		if httpResp.StatusCode != http.StatusOK {
			newAPIError := platformhttpx.RelayErrorHandler(c.Request.Context(), httpResp, false)
			if info.RelayMode == gatewaycontract.RelayModeChatCompletions &&
				shouldFallbackAfterStatus(info, bridgeChatToResponses, newAPIError) {
				bridgeError := executeChatToResponsesBridge(c, info, adaptor, request)
				if bridgeError == nil {
					rememberProtocolFallback(info, bridgeChatToResponses)
					return nil
				}
				return preferBridgeError(newAPIError, bridgeError)
			}
			platformhttpx.ResetStatusCode(newAPIError, statusCodeMappingStr)
			return newAPIError
		}
	}

	usage, newAPIError := adaptor.DoResponse(c, httpResp, info)
	if newAPIError != nil {
		platformhttpx.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}

	usageDTO := usage.(*dto.Usage)
	containAudioTokens := usageDTO.CompletionTokenDetails.AudioTokens > 0 || usageDTO.PromptTokensDetails.AudioTokens > 0
	containsAudioRatios := gatewaystore.ContainsAudioRatio(info.OriginModelName) || gatewaystore.ContainsAudioCompletionRatio(info.OriginModelName)
	if containAudioTokens && containsAudioRatios {
		billingapp.PostAudioConsumeQuota(c, info, usageDTO, "")
	} else {
		billingapp.PostTextConsumeQuota(c, info, usageDTO, nil)
	}
	return nil
}
