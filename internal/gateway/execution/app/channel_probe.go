package app

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	gatewayproviders "github.com/sh2001sh/new-api/internal/gateway/execution/providers"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/sh2001sh/new-api/types"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"
)

type channelTestOptions struct {
	UserID                 int
	BillUser               bool
	MarketplaceGroupID     string
	InternalGroup          string
	MarketplaceOwnerID     int
	CreditPoolPolicy       string
	MarketplaceMultiplier  float64
	MarketplaceModelPrices map[string]marketplacedomain.ChannelModelPrice
}

func testChannel(channel *gatewayschema.Channel, testModel string, endpointType string, isStream bool) channelTestResult {
	return testChannelWithOptions(channel, testModel, endpointType, isStream, channelTestOptions{UserID: 1})
}

func testChannelWithOptions(channel *gatewayschema.Channel, testModel string, endpointType string, isStream bool, options channelTestOptions) (result channelTestResult) {
	tik := time.Now()
	writer := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(writer)
	ctx.Request = &http.Request{Header: make(http.Header)}
	result.context = ctx
	if options.UserID <= 0 {
		options.UserID = 1
	}
	recordUserTestError := func(testErr error) {
		if !options.BillUser || testErr == nil || !constant.ErrorLogEnabled {
			return
		}
		other := map[string]interface{}{
			"status":          "failed",
			"error_message":   testErr.Error(),
			"is_channel_test": true,
		}
		channelID := 0
		if channel != nil {
			channelID = channel.Id
			other["channel_id"] = channel.Id
			other["channel_name"] = channel.Name
		}
		if ctx := result.context; ctx != nil {
			if ctx.Request != nil && ctx.Request.URL != nil {
				other["request_path"] = ctx.Request.URL.Path
			}
			other["total_duration_ms"] = time.Since(tik).Milliseconds()
			auditapp.RecordErrorLog(ctx, options.UserID, channelID, testModel, "模型测试", testErr.Error(), 0,
				int(time.Since(tik).Seconds()), isStream, ctx.GetString("group"), other)
		}
	}
	defer func() {
		if result.localErr != nil {
			recordUserTestError(result.localErr)
		}
	}()

	unsupportedTypes := []int{
		constant.ChannelTypeSunoAPI,
		constant.ChannelTypeKling,
		constant.ChannelTypeJimeng,
		constant.ChannelTypeDoubaoVideo,
		constant.ChannelTypeVidu,
	}
	if lo.Contains(unsupportedTypes, channel.Type) {
		channelTypeName := constant.GetChannelTypeName(channel.Type)
		return channelTestResult{
			localErr: fmt.Errorf("%s channel test is not supported", channelTypeName),
		}
	}

	testModel = normalizeChannelTestModel(channel, testModel)
	endpointType = normalizeChannelTestEndpoint(channel, testModel, endpointType)
	requestPath := resolveChannelTestRequestPath(channel, testModel, endpointType)
	if strings.HasPrefix(requestPath, "/v1/responses/compact") {
		testModel = gatewaystore.WithCompactModelSuffix(testModel)
	}

	ctx.Request = &http.Request{
		Method: "POST",
		URL:    buildChannelTestRequestURL(requestPath),
		Body:   nil,
		Header: make(http.Header),
	}

	if err := writeGatewayUserCacheToContext(ctx, options.UserID); err != nil {
		return channelTestResult{localErr: err}
	}
	httpctx.SetContextKey(ctx, constant.ContextKeyUserId, options.UserID)
	ctx.Set("id", options.UserID)
	ctx.Set("username", httpctx.GetContextKeyString(ctx, constant.ContextKeyUserName))
	ctx.Set("token_name", "模型测试")
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("channel", channel.Type)
	ctx.Set("base_url", channel.GetBaseURL())

	if options.MarketplaceGroupID != "" {
		httpctx.SetContextKey(ctx, constant.ContextKeyMarketplaceGroupID, options.MarketplaceGroupID)
		httpctx.SetContextKey(ctx, constant.ContextKeyMarketplaceOwnerID, options.MarketplaceOwnerID)
		httpctx.SetContextKey(ctx, constant.ContextKeyMarketplaceSourceType, marketplacedomain.SourceTypeMarketplaceUser)
		httpctx.SetContextKey(ctx, constant.ContextKeyMarketplaceCreditPolicy, options.CreditPoolPolicy)
		httpctx.SetContextKey(ctx, constant.ContextKeyMarketplaceMultiplier, options.MarketplaceMultiplier)
		httpctx.SetContextKey(ctx, constant.ContextKeyMarketplaceModelPrices, options.MarketplaceModelPrices)
		httpctx.SetContextKey(ctx, constant.ContextKeyUsingGroup, options.InternalGroup)
		httpctx.SetContextKey(ctx, constant.ContextKeyTokenGroup, options.InternalGroup)
		ctx.Set("group", options.InternalGroup)
	} else {
		group, _ := loadGatewayUserGroup(options.UserID, false)
		ctx.Set("group", group)
	}

	newAPIError := SetupContextForSelectedChannel(ctx, channel, testModel)
	if newAPIError != nil {
		return channelTestResult{
			context:     ctx,
			localErr:    newAPIError,
			newAPIError: newAPIError,
		}
	}

	relayFormat := resolveChannelTestRelayFormat(endpointType, ctx.Request.URL.Path)
	request := buildTestRequest(testModel, endpointType, channel, isStream)
	info, err := relaycommon.GenRelayInfo(ctx, relayFormat, request, nil)
	if err != nil {
		return channelTestResult{
			context:     ctx,
			localErr:    err,
			newAPIError: types.NewError(err, types.ErrorCodeGenRelayInfoFailed),
		}
	}

	info.IsChannelTest = true
	info.InitChannelMeta(ctx)
	ctx.Set(constant.RequestIdKey, info.RequestId)

	if err = attachTestBillingRequestInput(info, request); err != nil {
		return channelTestResult{
			context:     ctx,
			localErr:    err,
			newAPIError: types.NewError(err, types.ErrorCodeJsonMarshalFailed),
		}
	}
	if err = relaycommon.ModelMappedHelper(ctx, info, request); err != nil {
		return channelTestResult{
			context:     ctx,
			localErr:    err,
			newAPIError: types.NewError(err, types.ErrorCodeChannelModelMappedError),
		}
	}

	testModel = info.UpstreamModelName
	request.SetModelName(testModel)

	apiType, _ := constant.ChannelTypeToAPIType(channel.Type)
	if info.RelayMode == gatewaycontract.RelayModeResponsesCompact &&
		apiType != constant.APITypeOpenAI &&
		apiType != constant.APITypeCodex {
		err = fmt.Errorf("responses compaction test only supports openai/codex channels, got api type %d", apiType)
		return channelTestResult{
			context:     ctx,
			localErr:    err,
			newAPIError: types.NewError(err, types.ErrorCodeInvalidApiType),
		}
	}

	adaptor := gatewayproviders.NewSyncAdaptor(apiType)
	if adaptor == nil {
		err = fmt.Errorf("invalid api type: %d, adaptor is nil", apiType)
		return channelTestResult{
			context:     ctx,
			localErr:    err,
			newAPIError: types.NewError(err, types.ErrorCodeInvalidApiType),
		}
	}

	platformobservability.SysLog(fmt.Sprintf("testing channel %d with model %s , info %+v ", channel.Id, testModel, info.ToString()))

	priceData, err := relaycommon.ModelPriceHelper(ctx, info, 0, request.GetTokenCountMeta())
	if err != nil {
		return channelTestResult{
			context:     ctx,
			localErr:    err,
			newAPIError: types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest)),
		}
	}

	billingReserved := false
	billingSettled := false
	if options.BillUser && !priceData.FreeModel {
		if apiErr := billingapp.PreConsumeRelayBilling(ctx, priceData.QuotaToPreConsume, info); apiErr != nil {
			return channelTestResult{context: ctx, localErr: apiErr, newAPIError: apiErr}
		}
		billingReserved = true
	}
	defer func() {
		if options.BillUser && billingReserved && !billingSettled {
			if refundErr := billingapp.RefundRelayBillingSync(ctx, info); refundErr != nil {
				platformobservability.SysError(fmt.Sprintf("refund failed for marketplace channel test: %v", refundErr))
			}
		}
	}()

	adaptor.Init(info)
	convertedRequest, convertErr := convertChannelTestRequest(ctx, info, adaptor, request)
	if convertErr != nil {
		return channelTestResult{
			context:     ctx,
			localErr:    convertErr,
			newAPIError: types.NewError(convertErr, types.ErrorCodeConvertRequestFailed),
		}
	}

	jsonData, err := platformencoding.Marshal(convertedRequest)
	if err != nil {
		return channelTestResult{
			context:     ctx,
			localErr:    err,
			newAPIError: types.NewError(err, types.ErrorCodeJsonMarshalFailed),
		}
	}

	if len(info.ParamOverride) > 0 {
		jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
		if err != nil {
			if fixedErr, ok := relaycommon.AsParamOverrideReturnError(err); ok {
				return channelTestResult{
					context:     ctx,
					localErr:    fixedErr,
					newAPIError: relaycommon.NewAPIErrorFromParamOverride(fixedErr),
				}
			}
			return channelTestResult{
				context:     ctx,
				localErr:    err,
				newAPIError: types.NewError(err, types.ErrorCodeChannelParamOverrideInvalid),
			}
		}
	}

	requestBody := bytes.NewBuffer(jsonData)
	ctx.Request.Body = io.NopCloser(bytes.NewBuffer(jsonData))
	resp, err := adaptor.DoRequest(ctx, info, requestBody)
	if err != nil {
		return channelTestResult{
			context:     ctx,
			localErr:    err,
			newAPIError: types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError),
		}
	}

	var httpResp *http.Response
	if resp != nil {
		httpResp = resp.(*http.Response)
		if httpResp.StatusCode != http.StatusOK {
			err = platformhttpx.RelayErrorHandler(ctx.Request.Context(), httpResp, true)
			platformobservability.SysError(fmt.Sprintf(
				"channel test bad response: channel_id=%d name=%s type=%d model=%s endpoint_type=%s status=%d err=%v",
				channel.Id,
				channel.Name,
				channel.Type,
				testModel,
				endpointType,
				httpResp.StatusCode,
				err,
			))
			return channelTestResult{
				context:     ctx,
				localErr:    err,
				newAPIError: types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError),
			}
		}
	}

	usageAny, respErr := adaptor.DoResponse(ctx, httpResp, info)
	if respErr != nil {
		return channelTestResult{
			context:     ctx,
			localErr:    respErr,
			newAPIError: respErr,
		}
	}
	usage, usageErr := coerceTestUsage(usageAny, isStream, info.GetEstimatePromptTokens())
	if usageErr != nil {
		return channelTestResult{
			context:     ctx,
			localErr:    usageErr,
			newAPIError: types.NewOpenAIError(usageErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError),
		}
	}

	responseResult := writer.Result()
	respBody, err := readTestResponseBody(responseResult.Body, isStream)
	if err != nil {
		return channelTestResult{
			context:     ctx,
			localErr:    err,
			newAPIError: types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError),
		}
	}
	if bodyErr := validateTestResponseBody(respBody, isStream); bodyErr != nil {
		return channelTestResult{
			context:     ctx,
			localErr:    bodyErr,
			newAPIError: types.NewOpenAIError(bodyErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError),
		}
	}

	if options.BillUser {
		// Reuse the production text/image/embedding billing path so the selected
		// wallet or subscription and marketplace settlement are identical to an
		// ordinary request. It also writes the normal usage log with the user ID.
		if info.PriceData.UsePrice &&
			(info.RelayMode == gatewaycontract.RelayModeImagesGenerations || info.RelayMode == gatewaycontract.RelayModeImagesEdits) {
			if imageRequest, ok := request.(*dto.ImageRequest); ok {
				imageN := uint(1)
				if imageRequest.N != nil && *imageRequest.N > 0 {
					imageN = *imageRequest.N
				}
				if _, exists := info.PriceData.OtherRatios["n"]; !exists {
					info.PriceData.AddOtherRatio("n", float64(imageN))
				}
			}
		}
		billingapp.PostTextConsumeQuota(ctx, info, usage, nil)
		if info.Billing != nil && !info.BillingSettled {
			err := errors.New("测试计费结算失败")
			return channelTestResult{
				context:     ctx,
				localErr:    err,
				newAPIError: types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry()),
			}
		}
		billingSettled = info.Billing == nil || info.BillingSettled
		result.report = ChannelTestReport{
			QuotaCharged:  billingapp.BillingQuotaForLog(info, 0),
			LogCreated:    platformconfig.LogConsumeEnabled,
			RequestID:     info.RequestId,
			BillingSource: info.BillingSource,
		}
	} else {
		info.SetEstimatePromptTokens(usage.PromptTokens)
		quota, tieredResult := settleTestQuota(info, priceData, usage)
		tok := time.Now()
		milliseconds := tok.Sub(tik).Milliseconds()
		consumedTime := float64(milliseconds) / 1000.0
		other := buildTestLogOther(ctx, info, priceData, usage, tieredResult)
		// Automatic probes retain their historical non-billing audit record.
		auditapp.RecordConsumeLog(ctx, options.UserID, auditschema.RecordConsumeLogParams{
			ChannelId:        channel.Id,
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.CompletionTokens,
			ModelName:        info.OriginModelName,
			TokenName:        "模型测试",
			Quota:            quota,
			Content:          "模型测试",
			UseTimeSeconds:   int(consumedTime),
			IsStream:         info.IsStream,
			Group:            info.UsingGroup,
			Other:            other,
		})
	}
	platformobservability.SysLog(fmt.Sprintf("testing channel #%d, response: \n%s", channel.Id, string(respBody)))
	return result
}

func convertChannelTestRequest(ctx *gin.Context, info *relaycommon.RelayInfo, adaptor gatewayproviders.SyncAdaptor, request dto.Request) (any, error) {
	switch info.RelayMode {
	case gatewaycontract.RelayModeEmbeddings:
		embeddingReq, ok := request.(*dto.EmbeddingRequest)
		if !ok {
			return nil, errors.New("invalid embedding request type")
		}
		return adaptor.ConvertEmbeddingRequest(ctx, info, *embeddingReq)
	case gatewaycontract.RelayModeImagesGenerations:
		imageReq, ok := request.(*dto.ImageRequest)
		if !ok {
			return nil, errors.New("invalid image request type")
		}
		return adaptor.ConvertImageRequest(ctx, info, *imageReq)
	case gatewaycontract.RelayModeRerank:
		rerankReq, ok := request.(*dto.RerankRequest)
		if !ok {
			return nil, errors.New("invalid rerank request type")
		}
		return adaptor.ConvertRerankRequest(ctx, info.RelayMode, *rerankReq)
	case gatewaycontract.RelayModeResponses:
		responseReq, ok := request.(*dto.OpenAIResponsesRequest)
		if !ok {
			return nil, errors.New("invalid response request type")
		}
		return adaptor.ConvertOpenAIResponsesRequest(ctx, info, *responseReq)
	case gatewaycontract.RelayModeResponsesCompact:
		switch req := request.(type) {
		case *dto.OpenAIResponsesCompactionRequest:
			return adaptor.ConvertOpenAIResponsesRequest(ctx, info, dto.OpenAIResponsesRequest{
				Model:              req.Model,
				Input:              req.Input,
				Instructions:       req.Instructions,
				PreviousResponseID: req.PreviousResponseID,
			})
		case *dto.OpenAIResponsesRequest:
			return adaptor.ConvertOpenAIResponsesRequest(ctx, info, *req)
		default:
			return nil, errors.New("invalid response compaction request type")
		}
	default:
		generalReq, ok := request.(*dto.GeneralOpenAIRequest)
		if !ok {
			return nil, errors.New("invalid general request type")
		}
		return adaptor.ConvertOpenAIRequest(ctx, info, generalReq)
	}
}
