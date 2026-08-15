package providers

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/dto"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewaystream "github.com/sh2001sh/new-api/internal/gateway/stream"
	gatewaytranslation "github.com/sh2001sh/new-api/internal/gateway/translation"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"github.com/sh2001sh/new-api/internal/platform/tokenx"
	"github.com/sh2001sh/new-api/types"
)

func OaiChatToResponsesHandler(
	c *gin.Context,
	info *relaycommon.RelayInfo,
	resp *http.Response,
	request *dto.OpenAIResponsesRequest,
	meta *gatewaytranslation.ResponsesChatBridgeMeta,
) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	defer platformhttpx.CloseResponseBodyGracefully(resp)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	var chatResponse dto.OpenAITextResponse
	if err := platformencoding.Unmarshal(body, &chatResponse); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if openAIError := chatResponse.GetOpenAIError(); openAIError != nil && openAIError.Type != "" {
		return nil, types.WithOpenAIError(*openAIError, resp.StatusCode)
	}
	responsesResponse, usage, err := gatewaytranslation.ChatCompletionsResponseToResponsesResponse(&chatResponse, request, meta)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if usage == nil || usage.TotalTokens == 0 {
		usage = estimateChatResponseUsage(c, info, &chatResponse)
		responsesResponse.Usage = usage
	}
	responseBody, err := platformencoding.Marshal(responsesResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}
	platformhttpx.IOCopyBytesGracefully(c, resp, responseBody)
	return usage, nil
}

func OaiChatToResponsesStreamHandler(
	c *gin.Context,
	info *relaycommon.RelayInfo,
	resp *http.Response,
	request *dto.OpenAIResponsesRequest,
	meta *gatewaytranslation.ResponsesChatBridgeMeta,
) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	defer platformhttpx.CloseResponseBodyGracefully(resp)
	state := gatewaytranslation.NewChatToResponsesStreamState(request, meta)
	var streamErr *types.NewAPIError

	gatewaystream.ScanResponse(c, resp, info, func(data string, result *gatewaystream.Result) {
		var errorEnvelope dto.SimpleResponse
		if err := platformencoding.UnmarshalString(data, &errorEnvelope); err == nil && errorEnvelope.Error != nil {
			if openAIError := errorEnvelope.GetOpenAIError(); openAIError != nil {
				streamErr = types.WithOpenAIError(*openAIError, http.StatusInternalServerError)
			} else {
				streamErr = types.NewOpenAIError(fmt.Errorf("chat completions stream returned an error"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
			}
			result.Stop(streamErr)
			return
		}
		var chunk dto.ChatCompletionsStreamResponse
		if err := platformencoding.UnmarshalString(data, &chunk); err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
			result.Stop(streamErr)
			return
		}
		events, err := state.ConvertChunk(&chunk)
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
			result.Stop(streamErr)
			return
		}
		if err := writeResponsesBridgeEvents(c, events); err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
			result.Stop(streamErr)
		}
	})
	if streamErr != nil {
		return nil, streamErr
	}
	if state.Usage() == nil || state.Usage().TotalTokens == 0 {
		state.SetUsage(tokenx.ResponseText2Usage(c, state.UsageText(), info.UpstreamModelName, info.GetEstimatePromptTokens()))
	}
	events, usage, err := state.Finalize()
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if err := writeResponsesBridgeEvents(c, events); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	return usage, nil
}

func writeResponsesBridgeEvents(c *gin.Context, events []gatewaytranslation.ResponsesBridgeEvent) error {
	for _, event := range events {
		if err := gatewaystream.ObjectData(c, event); err != nil {
			return err
		}
	}
	return nil
}

func estimateChatResponseUsage(c *gin.Context, info *relaycommon.RelayInfo, response *dto.OpenAITextResponse) *dto.Usage {
	var text strings.Builder
	for _, choice := range response.Choices {
		text.WriteString(chatMessageReasoningText(choice.Message))
		text.WriteString(choice.Message.StringContent())
		for _, tool := range choice.Message.ParseToolCalls() {
			text.WriteString(tool.Function.Name)
			text.WriteString(tool.Function.Arguments)
		}
	}
	return tokenx.ResponseText2Usage(c, text.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
}

func chatMessageReasoningText(message dto.Message) string {
	if message.ReasoningContent != nil {
		return *message.ReasoningContent
	}
	if message.Reasoning != nil {
		return *message.Reasoning
	}
	return ""
}
