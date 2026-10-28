package providers

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewaystream "github.com/sh2001sh/new-api/internal/gateway/stream"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestOaiResponsesToChatStreamHandlerPreservesCacheWriteTokens(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`data: {"type":"response.reasoning_text.delta","delta":"thinking"}`,
		``,
		`data: {"type":"response.completed","response":{"usage":{"input_tokens":120,"output_tokens":30,"total_tokens":150,"input_tokens_details":{"cached_tokens":80,"cache_write_tokens":20,"cache_write_tokens_details":{"ttl_30m_tokens":12,"ttl_12h_tokens":8}},"output_tokens_details":{"reasoning_tokens":25}}}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	response := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"text/event-stream"}}}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"}, RelayFormat: types.RelayFormatOpenAI, IsStream: true}

	usage, err := OaiResponsesToChatStreamHandler(ctx, info, response)

	require.Nil(t, err)
	require.Equal(t, 20, usage.PromptTokensDetails.CachedCreationTokens)
	require.Equal(t, 12, usage.PromptTokensDetails.CacheWriteTokensDetails.TTL30mTokens)
	require.Equal(t, 8, usage.PromptTokensDetails.CacheWriteTokensDetails.TTL12hTokens)
	require.Equal(t, 30, usage.CompletionTokens)
	require.Equal(t, 25, usage.CompletionTokenDetails.ReasoningTokens)
	require.Contains(t, recorder.Body.String(), `"reasoning_content":"thinking"`)
}

func TestOaiResponsesToChatStreamHandlerMarksOutputCommittedAndMapsFailureToBadGateway(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"partial"}`,
		``,
		`data: {"type":"response.failed","response":{"error":{"type":"server_error","message":"upstream failed"}}}`,
		``,
	}, "\n")
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	response := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"text/event-stream"}}}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"}, RelayFormat: types.RelayFormatOpenAI, IsStream: true}

	_, apiErr := OaiResponsesToChatStreamHandler(ctx, info, response)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	require.Equal(t, gatewaystream.AttemptStageSemanticCommitted, gatewaystream.AttemptStageFromContext(ctx))
	require.True(t, ctx.GetBool(string(constant.ContextKeyUpstreamTerminalError)))
	require.Equal(t, "partial", info.ConversationResponseText)
}

func TestOaiResponsesToChatStreamHandlerFinalizesAfterNormalEOF(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"partial"}`,
		``,
	}, "\n")
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	response := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"text/event-stream"}}}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"}, RelayFormat: types.RelayFormatOpenAI, IsStream: true}
	info.SetEstimatePromptTokens(12)

	usage, apiErr := OaiResponsesToChatStreamHandler(ctx, info, response)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Equal(t, 12, usage.PromptTokens)
	require.Contains(t, recorder.Body.String(), `"finish_reason":"stop"`)
}

func TestOaiResponsesToChatCompletesWithoutWaitingForUpstreamEOF(t *testing.T) {
	previous := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = previous })
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.5"}, IsStream: true, RelayFormat: types.RelayFormatOpenAI}
	finished := make(chan struct{})
	var apiErr *types.NewAPIError
	go func() {
		defer close(finished)
		_, apiErr = OaiResponsesToChatStreamHandler(ctx, info, &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: reader})
	}()
	_, err := io.WriteString(writer, `data: {"type":"response.output_text.delta","delta":"hello"}`+"\n\n"+`data: {"type":"response.completed","response":{"usage":{"input_tokens":12,"output_tokens":3}}}`+"\n\n")
	require.NoError(t, err)
	select {
	case <-finished:
	case <-time.After(time.Second):
		_ = writer.Close()
		<-finished
		t.Fatal("completed response waited for an upstream EOF")
	}
	require.Nil(t, apiErr)
}

func TestOaiResponsesToChatStreamHandlerMapsIncompleteResponseToLength(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"partial"}`,
		``,
		`data: {"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reasoning":"max_output_tokens"}}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	response := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"text/event-stream"}}}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"}, RelayFormat: types.RelayFormatOpenAI, IsStream: true}

	_, err := OaiResponsesToChatStreamHandler(ctx, info, response)
	require.Nil(t, err)
	require.Contains(t, recorder.Body.String(), `"finish_reason":"length"`)
}
