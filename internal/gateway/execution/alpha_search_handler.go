package execution

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewaytranslation "github.com/sh2001sh/new-api/internal/gateway/translation"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	"github.com/sh2001sh/new-api/types"
)

// AlphaSearchHelper proxies the standalone search endpoint used by Codex clients.
func AlphaSearchHelper(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	info.InitChannelMeta(c)
	jsonData, newAPIError := prepareAlphaSearchRequest(c, info)
	if newAPIError != nil {
		return newAPIError
	}
	if info.ApiType == constant.APITypeOpenAI && info.ChannelType != constant.ChannelTypeAzure {
		return executePortableAlphaSearch(c, info, jsonData)
	}

	logger.LogDebug(c, "alpha search request body: %s", jsonData)
	body, size, closer, err := relaycommon.NewOutboundJSONBody(jsonData)
	if err != nil {
		return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	defer closer.Close()
	info.UpstreamRequestBodySize = size

	adaptor := NewSyncAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)
	response, err := adaptor.DoRequest(c, info, body)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	httpResponse, ok := response.(*http.Response)
	if !ok || httpResponse == nil {
		return types.NewOpenAIError(errors.New("invalid http response"), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	defer platformhttpx.CloseResponseBodyGracefully(httpResponse)
	if httpResponse.StatusCode < http.StatusOK || httpResponse.StatusCode >= http.StatusMultipleChoices {
		newAPIError := platformhttpx.RelayErrorHandler(c.Request.Context(), httpResponse, false)
		platformhttpx.ResetStatusCode(newAPIError, c.GetString("status_code_mapping"))
		return newAPIError
	}

	return writeAlphaSearchResponse(c, info, httpResponse)
}

func executePortableAlphaSearch(c *gin.Context, info *relaycommon.RelayInfo, alphaSearchBody []byte) *types.NewAPIError {
	jsonData, err := buildPortableAlphaSearchResponsesBody(alphaSearchBody, info.UpstreamModelName)
	if err != nil {
		return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	portableInfo := *info
	portableInfo.RelayMode = gatewaycontract.RelayModeResponses
	portableInfo.RelayFormat = types.RelayFormatOpenAIResponses
	portableInfo.RequestURLPath = "/v1/responses"
	portableInfo.IsStream = false

	body, size, closer, err := relaycommon.NewOutboundJSONBody(jsonData)
	if err != nil {
		return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	defer closer.Close()
	info.UpstreamRequestBodySize = size
	adaptor := NewSyncAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(&portableInfo)
	response, err := adaptor.DoRequest(c, &portableInfo, body)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	httpResponse, ok := response.(*http.Response)
	if !ok || httpResponse == nil {
		return types.NewOpenAIError(errors.New("invalid http response"), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	defer platformhttpx.CloseResponseBodyGracefully(httpResponse)
	if httpResponse.StatusCode < http.StatusOK || httpResponse.StatusCode >= http.StatusMultipleChoices {
		newAPIError := platformhttpx.RelayErrorHandler(c.Request.Context(), httpResponse, false)
		platformhttpx.ResetStatusCode(newAPIError, c.GetString("status_code_mapping"))
		return newAPIError
	}
	responseBody, err := io.ReadAll(httpResponse.Body)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusBadGateway)
	}
	var responsesResponse dto.OpenAIResponsesResponse
	if err := platformencoding.Unmarshal(responseBody, &responsesResponse); err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	if oaiError := responsesResponse.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return types.WithOpenAIError(*oaiError, httpResponse.StatusCode)
	}
	output := strings.TrimSpace(gatewaytranslation.ExtractOutputTextFromResponses(&responsesResponse))
	if output == "" {
		return types.NewOpenAIError(errors.New("portable web search returned no text output"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	result, err := platformencoding.Marshal(map[string]any{"output": output})
	if err != nil {
		return types.NewError(err, types.ErrorCodeConvertRequestFailed)
	}
	usage := &dto.Usage{}
	if responsesResponse.Usage != nil {
		usage.PromptTokens = responsesResponse.Usage.InputTokens
		usage.CompletionTokens = responsesResponse.Usage.OutputTokens
		usage.TotalTokens = responsesResponse.Usage.TotalTokens
		if responsesResponse.Usage.InputTokensDetails != nil {
			usage.PromptTokensDetails.CachedTokens = responsesResponse.Usage.InputTokensDetails.CachedTokens
			usage.PromptTokensDetails.CachedCreationTokens = responsesResponse.Usage.InputTokensDetails.GetCachedCreationTokens()
		}
	}
	info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount = 1
	billingapp.PostTextConsumeQuota(c, info, usage, nil)
	c.Data(http.StatusOK, "application/json; charset=utf-8", result)
	return nil
}

func buildPortableAlphaSearchResponsesBody(rawBody []byte, model string) ([]byte, error) {
	if len(rawBody) == 0 {
		return nil, errors.New("empty alpha search request body")
	}
	var request map[string]json.RawMessage
	if err := platformencoding.Unmarshal(rawBody, &request); err != nil {
		return nil, err
	}
	if strings.TrimSpace(model) == "" {
		return nil, errors.New("alpha search model is empty")
	}
	tool := map[string]any{"type": "web_search"}
	if settingsRaw := request["settings"]; len(settingsRaw) > 0 && string(settingsRaw) != "null" {
		var settings map[string]json.RawMessage
		if err := platformencoding.Unmarshal(settingsRaw, &settings); err == nil {
			for _, key := range []string{"search_context_size", "user_location", "filters"} {
				if value := settings[key]; len(value) > 0 && string(value) != "null" {
					tool[key] = value
				}
			}
		}
	}
	prompt := "Execute this standalone web search request using the web search tool. Follow every command in order and return the useful results with source URLs. Request JSON:\n" + string(rawBody)
	body := map[string]any{
		"model":  model,
		"input":  prompt,
		"tools":  []any{tool},
		"stream": false,
		"store":  false,
	}
	for _, key := range []string{"reasoning", "max_output_tokens"} {
		if value := request[key]; len(value) > 0 && string(value) != "null" {
			body[key] = value
		}
	}
	return platformencoding.Marshal(body)
}

func prepareAlphaSearchRequest(c *gin.Context, info *relaycommon.RelayInfo) ([]byte, *types.NewAPIError) {
	if info.ApiType != constant.APITypeOpenAI && info.ApiType != constant.APITypeCodex {
		return nil, types.NewError(errors.New("channel does not support /v1/alpha/search"), types.ErrorCodeInvalidRequest)
	}
	if info.ChannelType == constant.ChannelTypeAzure {
		return nil, types.NewError(errors.New("azure channel does not support /v1/alpha/search"), types.ErrorCodeInvalidRequest)
	}
	request, ok := info.Request.(*dto.AlphaSearchRequest)
	if !ok {
		return nil, types.NewErrorWithStatusCode(
			fmt.Errorf("invalid request type, expected *dto.AlphaSearchRequest, got %T", info.Request),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if err := relaycommon.ModelMappedHelper(c, info, request); err != nil {
		return nil, types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}
	jsonData, err := buildAlphaSearchRequestBody(request.RawBody, info.OriginModelName, info.UpstreamModelName)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	if len(info.ParamOverride) == 0 {
		return jsonData, nil
	}
	jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
	if err != nil {
		return nil, newAPIErrorFromParamOverride(err)
	}
	return jsonData, nil
}

func writeAlphaSearchResponse(c *gin.Context, info *relaycommon.RelayInfo, response *http.Response) *types.NewAPIError {
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusBadGateway)
	}
	platformhttpx.IOCopyBytesGracefully(c, response, responseBody)
	info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount = 1
	billingapp.PostTextConsumeQuota(c, info, &dto.Usage{}, nil)
	return nil
}

func buildAlphaSearchRequestBody(rawBody []byte, originModel, upstreamModel string) ([]byte, error) {
	if len(rawBody) == 0 {
		return nil, errors.New("empty alpha search request body")
	}
	if upstreamModel == "" || upstreamModel == originModel {
		return rawBody, nil
	}
	var body map[string]any
	if err := platformencoding.Unmarshal(rawBody, &body); err != nil {
		return nil, err
	}
	body["model"] = upstreamModel
	return platformencoding.Marshal(body)
}
