package openai

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	helper "github.com/sh2001sh/new-api/internal/gateway/stream"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	"github.com/sh2001sh/new-api/internal/platform/tokenx"
	"github.com/sh2001sh/new-api/types"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

const (
	responsesPreOutputEventLimit = 128
	responsesPreOutputByteLimit  = 1 << 20
)

type bufferedResponsesStreamEvent struct {
	response dto.ResponsesStreamResponse
	data     string
}

func OaiResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer platformhttpx.CloseResponseBodyGracefully(resp)

	// read response body
	var responsesResponse dto.OpenAIResponsesResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	err = platformencoding.Unmarshal(responseBody, &responsesResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := responsesResponse.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	if responsesResponse.HasImageGenerationCall() {
		c.Set("image_generation_call", true)
		c.Set("image_generation_call_quality", responsesResponse.GetQuality())
		c.Set("image_generation_call_size", responsesResponse.GetSize())
	}

	// 写入新的 response body
	info.ConversationResponseText = responsesOutputText(&responsesResponse)
	platformhttpx.IOCopyBytesGracefully(c, resp, responseBody)

	// compute usage
	usage := dto.Usage{}
	if responsesResponse.Usage != nil {
		usage.PromptTokens = responsesResponse.Usage.InputTokens
		usage.CompletionTokens = responsesResponse.Usage.OutputTokens
		usage.TotalTokens = responsesResponse.Usage.TotalTokens
		if responsesResponse.Usage.InputTokensDetails != nil {
			usage.PromptTokensDetails.CachedTokens = responsesResponse.Usage.InputTokensDetails.CachedTokens
			usage.PromptTokensDetails.CachedCreationTokens = responsesResponse.Usage.InputTokensDetails.GetCachedCreationTokens()
		}
	}
	if info == nil || info.ResponsesUsageInfo == nil || info.ResponsesUsageInfo.BuiltInTools == nil {
		return &usage, nil
	}
	// 解析 Tools 用量
	for _, tool := range responsesResponse.Tools {
		buildToolinfo, ok := info.ResponsesUsageInfo.BuiltInTools[platformencoding.Interface2String(tool["type"])]
		if !ok || buildToolinfo == nil {
			logger.LogError(c, fmt.Sprintf("BuiltInTools not found for tool type: %v", tool["type"]))
			continue
		}
		buildToolinfo.CallCount++
	}
	return &usage, nil
}

func OaiResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, "invalid response or response body")
		return nil, types.NewError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse)
	}

	defer platformhttpx.CloseResponseBodyGracefully(resp)
	helper.MarkAttemptConnected(c)
	// Responses streams often begin with lifecycle events before model content.
	// A disconnect in that phase can be retried without duplicating output.
	c.Set(string(constant.ContextKeyResponsesStreamRetrySafe), true)
	helper.MarkAttemptBootstrap(c)

	var usage = &dto.Usage{}
	var responseTextBuilder strings.Builder
	var sawSemanticOutput atomic.Bool
	var sawResponseCompleted atomic.Bool
	var firstOutputTimedOut atomic.Bool
	var preOutputEvents []bufferedResponsesStreamEvent
	preOutputEventsBuffered := 0
	preOutputEventsDropped := 0
	preOutputBytes := 0
	if responsesRequestUsesImageGeneration(info) {
		relaycommon.MarkImageGenerationRequest(c)
	}
	firstOutputTimeout := relaycommon.StreamFirstOutputTimeoutForRequest(c, info.OriginModelName, info.GetEstimatePromptTokens())
	var firstOutputTimer *time.Timer
	if firstOutputTimeout > 0 {
		firstOutputTimer = time.AfterFunc(firstOutputTimeout, func() {
			if !sawSemanticOutput.Load() && !sawResponseCompleted.Load() {
				firstOutputTimedOut.Store(true)
				if info.StreamStatus != nil {
					info.StreamStatus.SetEndReason(
						gatewaycontract.StreamEndReasonTimeout,
						fmt.Errorf("semantic output timeout after %s", firstOutputTimeout),
					)
				}
				_ = resp.Body.Close()
			}
		})
		defer firstOutputTimer.Stop()
	}

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {

		// 检查当前数据是否包含 completed 状态和 usage 信息
		var streamResponse dto.ResponsesStreamResponse
		if err := platformencoding.UnmarshalString(data, &streamResponse); err != nil {
			logger.LogError(c, "failed to unmarshal stream response: "+err.Error())
			sr.Error(err)
			return
		}
		semanticOutput := hasResponsesStreamContent(streamResponse)
		if semanticOutput {
			if sawSemanticOutput.CompareAndSwap(false, true) {
				if info.FirstByteTrace != nil {
					info.FirstByteTrace.MarkFirstSemanticReadAt(sr.ReceivedAt())
				}
				info.SetFirstSemanticResponseTime()
				if firstOutputTimer != nil {
					firstOutputTimer.Stop()
				}
				if err := flushBufferedResponsesStreamEvents(c, info, preOutputEvents); err != nil {
					logger.LogError(c, "failed to flush responses lifecycle events: "+err.Error())
					sr.Stop(err)
					return
				}
				preOutputEvents = nil
			}
			helper.MarkSemanticCommitted(c)
			if err := sendResponsesStreamData(c, info, streamResponse, data); err != nil {
				logger.LogError(c, "failed to write responses stream response: "+err.Error())
				sr.Stop(err)
				return
			}
		} else if streamResponse.Type == "response.completed" {
			sawResponseCompleted.Store(true)
			if firstOutputTimer != nil {
				firstOutputTimer.Stop()
			}
			if err := flushBufferedResponsesStreamEvents(c, info, preOutputEvents); err != nil {
				logger.LogError(c, "failed to flush responses lifecycle events: "+err.Error())
				sr.Stop(err)
				return
			}
			preOutputEvents = nil
			if err := sendResponsesStreamData(c, info, streamResponse, data); err != nil {
				logger.LogError(c, "failed to write responses stream response: "+err.Error())
				sr.Stop(err)
				return
			}
		} else {
			preOutputEventsDropped += appendBufferedResponsesStreamEvent(&preOutputEvents, &preOutputBytes, streamResponse, data)
			preOutputEventsBuffered = len(preOutputEvents)
		}
		switch streamResponse.Type {
		case "response.completed":
			sr.Done()
			if streamResponse.Response != nil {
				if streamResponse.Response.Usage != nil {
					if streamResponse.Response.Usage.InputTokens != 0 {
						usage.PromptTokens = streamResponse.Response.Usage.InputTokens
					}
					if streamResponse.Response.Usage.OutputTokens != 0 {
						usage.CompletionTokens = streamResponse.Response.Usage.OutputTokens
					}
					if streamResponse.Response.Usage.TotalTokens != 0 {
						usage.TotalTokens = streamResponse.Response.Usage.TotalTokens
					}
					if streamResponse.Response.Usage.InputTokensDetails != nil {
						usage.PromptTokensDetails.CachedTokens = streamResponse.Response.Usage.InputTokensDetails.CachedTokens
						usage.PromptTokensDetails.CachedCreationTokens = streamResponse.Response.Usage.InputTokensDetails.GetCachedCreationTokens()
					}
				}
				if streamResponse.Response.HasImageGenerationCall() {
					c.Set("image_generation_call", true)
					c.Set("image_generation_call_quality", streamResponse.Response.GetQuality())
					c.Set("image_generation_call_size", streamResponse.Response.GetSize())
				}
			}
		case "response.output_text.delta":
			// 处理输出文本
			responseTextBuilder.WriteString(streamResponse.Delta)
		case dto.ResponsesOutputTypeItemDone:
			// 函数调用处理
			if streamResponse.Item != nil {
				switch streamResponse.Item.Type {
				case dto.BuildInCallWebSearchCall:
					if info != nil && info.ResponsesUsageInfo != nil && info.ResponsesUsageInfo.BuiltInTools != nil {
						if webSearchTool, exists := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview]; exists && webSearchTool != nil {
							webSearchTool.CallCount++
						}
					}
				}
			}
		}
	})
	c.Set("responses_stream_lifecycle", map[string]interface{}{
		"received_events":            info.ReceivedResponseCount,
		"semantic_output_seen":       sawSemanticOutput.Load(),
		"response_completed_seen":    sawResponseCompleted.Load(),
		"pre_output_events_buffered": preOutputEventsBuffered,
		"pre_output_events_dropped":  preOutputEventsDropped,
		"first_output_timeout_ms":    firstOutputTimeout.Milliseconds(),
		"first_output_timed_out":     firstOutputTimedOut.Load(),
	})

	if firstOutputTimedOut.Load() && !sawSemanticOutput.Load() {
		return nil, types.NewOpenAIError(
			fmt.Errorf("upstream produced no semantic output before %s", firstOutputTimeout),
			types.ErrorCodeChannelResponseTimeExceeded,
			http.StatusGatewayTimeout,
		)
	}

	if !sawResponseCompleted.Load() {
		options := make([]types.NewAPIErrorOptions, 0, 1)
		if c.GetBool(string(constant.ContextKeyStreamContentDelivered)) {
			options = append(options, types.ErrOptionWithSkipRetry())
		}
		return nil, types.NewOpenAIError(
			fmt.Errorf("responses stream closed before response.completed"),
			types.ErrorCodeBadResponse,
			http.StatusBadGateway,
			options...,
		)
	}

	if usage.CompletionTokens == 0 {
		// 计算输出文本的 token 数量
		tempStr := responseTextBuilder.String()
		if len(tempStr) > 0 {
			// 非正常结束，使用输出文本的 token 数量
			completionTokens := tokenx.CountTextToken(tempStr, info.UpstreamModelName)
			usage.CompletionTokens = completionTokens
		}
	}

	if usage.PromptTokens == 0 && usage.CompletionTokens != 0 {
		usage.PromptTokens = info.GetEstimatePromptTokens()
	}

	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	info.ConversationResponseText = responseTextBuilder.String()
	helper.MarkAttemptCompleted(c)

	return usage, nil
}

func responsesRequestUsesImageGeneration(info *relaycommon.RelayInfo) bool {
	if info == nil {
		return false
	}
	if gatewaycontract.IsImageGenerationModel(info.OriginModelName) {
		return true
	}
	request, ok := info.Request.(*dto.OpenAIResponsesRequest)
	if !ok {
		return false
	}
	for _, tool := range request.GetToolsMap() {
		if strings.EqualFold(strings.TrimSpace(platformencoding.Interface2String(tool["type"])), "image_generation") {
			return true
		}
	}
	return false
}

// appendBufferedResponsesStreamEvent keeps lifecycle data bounded until the
// first model-visible event. Overflowing lifecycle-only events are discarded,
// not treated as an upstream failure, so large Responses continuations remain
// retry-safe and can still reach their first output.
func appendBufferedResponsesStreamEvent(events *[]bufferedResponsesStreamEvent, size *int, response dto.ResponsesStreamResponse, data string) (dropped int) {
	if len(data) > responsesPreOutputByteLimit {
		return 1
	}
	for len(*events) > 0 && (len(*events) >= responsesPreOutputEventLimit || *size+len(data) > responsesPreOutputByteLimit) {
		dropIndex := 0
		if (*events)[0].response.Type == "response.created" && len(*events) > 1 {
			dropIndex = 1
		}
		*size -= len((*events)[dropIndex].data)
		*events = append((*events)[:dropIndex], (*events)[dropIndex+1:]...)
		dropped++
	}
	if len(*events) >= responsesPreOutputEventLimit || *size+len(data) > responsesPreOutputByteLimit {
		return dropped + 1
	}
	*events = append(*events, bufferedResponsesStreamEvent{response: response, data: data})
	*size += len(data)
	return dropped
}

func flushBufferedResponsesStreamEvents(c *gin.Context, info *relaycommon.RelayInfo, events []bufferedResponsesStreamEvent) error {
	for _, event := range events {
		if err := sendResponsesStreamData(c, info, event.response, event.data); err != nil {
			return err
		}
	}
	return nil
}

func hasResponsesStreamContent(streamResponse dto.ResponsesStreamResponse) bool {
	if streamResponse.Delta != "" && strings.HasSuffix(streamResponse.Type, ".delta") {
		return true
	}
	return streamResponse.Type == "response.output_item.added" &&
		streamResponse.Item != nil && streamResponse.Item.Type == "function_call"
}

func responsesOutputText(response *dto.OpenAIResponsesResponse) string {
	if response == nil {
		return ""
	}
	var parts []string
	for _, output := range response.Output {
		for _, content := range output.Content {
			if strings.TrimSpace(content.Text) != "" {
				parts = append(parts, content.Text)
			}
		}
	}
	return strings.Join(parts, "\n")
}
