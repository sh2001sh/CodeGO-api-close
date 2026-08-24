package execution

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
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
