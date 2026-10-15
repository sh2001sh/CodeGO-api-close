package execution

import (
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/dto"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	gatewayproviders "github.com/sh2001sh/new-api/internal/gateway/execution/providers"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewaytranslation "github.com/sh2001sh/new-api/internal/gateway/translation"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"github.com/sh2001sh/new-api/types"
)

func responsesViaChatCompletions(
	c *gin.Context,
	info *relaycommon.RelayInfo,
	adaptor gatewayproviders.SyncAdaptor,
	request *dto.OpenAIResponsesRequest,
) (*dto.Usage, *types.NewAPIError) {
	overriddenRequest, newAPIError := prepareResponsesBridgeRequest(info, request)
	if newAPIError != nil {
		return nil, newAPIError
	}
	chatRequest, meta, err := gatewaytranslation.ResponsesRequestToChatCompletionsRequest(overriddenRequest)
	if err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	info.AppendRequestConversion(types.RelayFormatOpenAI)

	restore := useChatCompletionsRelayMode(info)
	defer restore()
	convertedRequest, err := adaptor.ConvertOpenAIRequest(c, info, chatRequest)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)
	if finalFormat := info.GetFinalRequestRelayFormat(); finalFormat != types.RelayFormatOpenAI {
		return nil, types.NewErrorWithStatusCode(
			newBridgeFormatError(finalFormat), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry(),
		)
	}
	requestBody, closeBody, newAPIError := marshalResponsesBridgeOutbound(info, convertedRequest)
	if newAPIError != nil {
		return nil, newAPIError
	}
	defer closeBody()
	if info.FirstByteTrace != nil {
		info.FirstByteTrace.MarkRequestConversionDone()
	}

	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	if resp == nil {
		return nil, types.NewOpenAIError(nil, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	httpResponse := resp.(*http.Response)
	statusCodeMapping := c.GetString("status_code_mapping")
	info.IsStream = info.IsStream || strings.HasPrefix(httpResponse.Header.Get("Content-Type"), "text/event-stream")
	if httpResponse.StatusCode != http.StatusOK {
		newAPIError := platformhttpx.RelayErrorHandler(c.Request.Context(), httpResponse, false)
		platformhttpx.ResetStatusCode(newAPIError, statusCodeMapping)
		return nil, newAPIError
	}
	if info.IsStream {
		return gatewayproviders.OaiChatToResponsesStreamHandler(c, info, httpResponse, overriddenRequest, meta)
	}
	return gatewayproviders.OaiChatToResponsesHandler(c, info, httpResponse, overriddenRequest, meta)
}

func prepareResponsesBridgeRequest(info *relaycommon.RelayInfo, request *dto.OpenAIResponsesRequest) (*dto.OpenAIResponsesRequest, *types.NewAPIError) {
	jsonData, err := platformencoding.Marshal(request)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	jsonData, err = relaycommon.RemoveDisabledFields(jsonData, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	if len(info.ParamOverride) > 0 {
		jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
		if err != nil {
			return nil, newAPIErrorFromParamOverride(err)
		}
	}
	var overridden dto.OpenAIResponsesRequest
	if err := platformencoding.Unmarshal(jsonData, &overridden); err != nil {
		return nil, types.NewError(err, types.ErrorCodeChannelParamOverrideInvalid, types.ErrOptionWithSkipRetry())
	}
	return &overridden, nil
}

func useChatCompletionsRelayMode(info *relaycommon.RelayInfo) func() {
	savedMode := info.RelayMode
	savedPath := info.RequestURLPath
	info.RelayMode = gatewaycontract.RelayModeChatCompletions
	info.RequestURLPath = "/v1/chat/completions"
	return func() {
		info.RelayMode = savedMode
		info.RequestURLPath = savedPath
	}
}

func marshalResponsesBridgeOutbound(info *relaycommon.RelayInfo, request any) (io.Reader, func(), *types.NewAPIError) {
	jsonData, err := platformencoding.Marshal(request)
	if err != nil {
		return nil, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	jsonData, err = relaycommon.RemoveDisabledFields(jsonData, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
	if err != nil {
		return nil, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	body, size, closer, err := relaycommon.NewOutboundJSONBody(jsonData)
	if err != nil {
		return nil, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	info.UpstreamRequestBodySize = size
	return body, func() { _ = closer.Close() }, nil
}

func newBridgeFormatError(format types.RelayFormat) error {
	return &responsesBridgeFormatError{format: format}
}

type responsesBridgeFormatError struct{ format types.RelayFormat }

func (e *responsesBridgeFormatError) Error() string {
	return "responses-to-chat bridge requires an OpenAI Chat Completions wire adaptor, got " + string(e.format)
}
