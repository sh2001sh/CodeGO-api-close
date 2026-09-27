package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	"github.com/stretchr/testify/require"
)

func TestHandleFinalResponseClaudeFinalizesDoneWithoutFinishReason(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 5
	t.Cleanup(func() { constant.StreamingTimeout = oldStreamingTimeout })
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	info := relaycommon.GenRelayInfoClaude(context, &dto.BaseRequest{})
	require.NotNil(t, info)
	info.ChannelMeta = &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"}

	upstream := "data: {\"id\":\"chatcmpl-test\",\"model\":\"gpt-test\",\"choices\":[{\"delta\":{\"content\":\"hello\"},\"index\":0}]}\n\n" +
		"data: [DONE]\n\n"
	usage, streamErr := OaiStreamHandler(context, info, &http.Response{
		Body:   io.NopCloser(strings.NewReader(upstream)),
		Header: make(http.Header),
	})
	require.Nil(t, streamErr)
	require.NotNil(t, usage)

	body := recorder.Body.String()
	messageStart := strings.Index(body, "event: message_start")
	contentStop := strings.Index(body, "event: content_block_stop")
	messageDelta := strings.Index(body, "event: message_delta")
	messageStop := strings.Index(body, "event: message_stop")
	require.GreaterOrEqual(t, messageStart, 0)
	require.Greater(t, contentStop, messageStart)
	require.GreaterOrEqual(t, contentStop, 0)
	require.Greater(t, messageDelta, contentStop)
	require.Greater(t, messageStop, messageDelta)
	require.Contains(t, body, `"stop_reason":"end_turn"`)
	require.Equal(t, 1, strings.Count(body, "event: message_stop"))
	require.True(t, info.ClaudeConvertInfo.Done)
}

func TestExtractLastValidStreamUsageFindsUsageBeforeFinalChunk(t *testing.T) {
	streamItems := []string{
		`{"id":"chatcmpl-test","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"lookup","arguments":"{\"q\":"}}]}}]}`,
		`{"id":"chatcmpl-test","choices":[],"usage":{"prompt_tokens":4113,"completion_tokens":248,"total_tokens":4361}}`,
		`{"id":"chatcmpl-test","choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	}

	usage, found := extractLastValidStreamUsage(streamItems)

	require.True(t, found)
	require.Equal(t, 4113, usage.PromptTokens)
	require.Equal(t, 248, usage.CompletionTokens)
	require.Equal(t, 4361, usage.TotalTokens)
}

func TestExtractLastValidStreamUsageSkipsInvalidUsage(t *testing.T) {
	streamItems := []string{
		`{"choices":[],"usage":{"prompt_tokens":4113,"completion_tokens":248,"total_tokens":4361}}`,
		`{"choices":[],"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`,
	}

	usage, found := extractLastValidStreamUsage(streamItems)

	require.True(t, found)
	require.Equal(t, 4113, usage.PromptTokens)
	require.Equal(t, 248, usage.CompletionTokens)
}

func TestOaiStreamHandlerUsesUsageFromNonFinalChunk(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 5
	t.Cleanup(func() { constant.StreamingTimeout = oldStreamingTimeout })
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := relaycommon.GenRelayInfoOpenAI(context, &dto.GeneralOpenAIRequest{})
	require.NotNil(t, info)
	info.ChannelMeta = &relaycommon.ChannelMeta{UpstreamModelName: "gemini-3.8-flash"}
	info.ShouldIncludeUsage = true

	upstream := "data: {\"id\":\"chatcmpl-test\",\"model\":\"gemini-3.8-flash\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"lookup\",\"arguments\":\"{}\"}}]}}]}\n\n" +
		"data: {\"id\":\"chatcmpl-test\",\"model\":\"gemini-3.8-flash\",\"choices\":[],\"usage\":{\"prompt_tokens\":4113,\"completion_tokens\":248,\"total_tokens\":4361}}\n\n" +
		"data: {\"id\":\"chatcmpl-test\",\"model\":\"gemini-3.8-flash\",\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: [DONE]\n\n"

	usage, streamErr := OaiStreamHandler(context, info, &http.Response{
		Body:   io.NopCloser(strings.NewReader(upstream)),
		Header: make(http.Header),
	})

	require.Nil(t, streamErr)
	require.NotNil(t, usage)
	require.Equal(t, 4113, usage.PromptTokens)
	require.Equal(t, 248, usage.CompletionTokens)
	require.Equal(t, 4361, usage.TotalTokens)
}
