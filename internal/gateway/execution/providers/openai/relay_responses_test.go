package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewaystream "github.com/sh2001sh/new-api/internal/gateway/stream"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestOaiResponsesStreamHandlerReturnsErrorWithoutResponseCompleted(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		``,
	}, "\n")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-5.5",
		},
		IsStream: true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)
	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, http.StatusBadGateway, err.StatusCode)
	require.Equal(t, types.ErrorCodeBadResponse, err.GetErrorCode())
	require.True(t, types.IsSkipRetryError(err))
	require.Contains(t, err.Error(), "response.completed")
	require.Contains(t, recorder.Body.String(), `"type":"response.output_text.delta"`)
}

func TestOaiResponsesStreamHandlerAllowsRetryBeforeContent(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_123"}}`,
		``,
	}, "\n")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"text/event-stream"}}}

	usage, err := OaiResponsesStreamHandler(c, &relaycommon.RelayInfo{IsStream: true}, resp)
	require.Nil(t, usage)
	require.NotNil(t, err)
	require.False(t, types.IsSkipRetryError(err))
	require.True(t, c.GetBool(string(constant.ContextKeyResponsesStreamRetrySafe)))
	require.False(t, c.GetBool(string(constant.ContextKeyStreamContentDelivered)))
	require.Equal(t, gatewaystream.AttemptStageBootstrap, gatewaystream.AttemptStageFromContext(c))
	require.Empty(t, recorder.Body.String())
}

func TestOaiResponsesStreamHandlerMarksCancelledClient(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	requestContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader, writer := io.Pipe()
	go func() {
		_, _ = io.WriteString(writer, `data: {"type":"response.created","response":{"id":"resp_123"}}`+"\n\n")
		cancel()
		_ = writer.Close()
	}()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(requestContext)
	resp := &http.Response{StatusCode: http.StatusOK, Body: reader, Header: http.Header{"Content-Type": []string{"text/event-stream"}}}

	_, err := OaiResponsesStreamHandler(c, &relaycommon.RelayInfo{IsStream: true}, resp)

	require.NotNil(t, err)
	require.True(t, c.GetBool(string(constant.ContextKeyClientGone)))
}

func TestOaiResponsesStreamHandlerTimesOutBeforeSemanticOutput(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	oldFirstByteTimeout := constant.StreamingFirstByteTimeout
	constant.StreamingTimeout = 30
	constant.StreamingFirstByteTimeout = 1
	t.Cleanup(func() {
		constant.StreamingTimeout = oldTimeout
		constant.StreamingFirstByteTimeout = oldFirstByteTimeout
	})

	reader, writer := io.Pipe()
	defer writer.Close()
	go func() {
		_, _ = io.WriteString(writer, `data: {"type":"response.created","response":{"id":"resp_123"}}`+"\n\n")
	}()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{OriginModelName: "gpt-5.6-sol", IsStream: true}
	resp := &http.Response{StatusCode: http.StatusOK, Body: reader, Header: http.Header{"Content-Type": []string{"text/event-stream"}}}

	started := time.Now()
	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, http.StatusGatewayTimeout, err.StatusCode)
	require.Equal(t, types.ErrorCodeChannelResponseTimeExceeded, err.GetErrorCode())
	require.False(t, types.IsSkipRetryError(err))
	require.Less(t, time.Since(started), 3*time.Second)
	require.Empty(t, recorder.Body.String())
}

func TestOaiResponsesStreamHandlerFlushesLifecycleBeforeSemanticOutput(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_123"}}`,
		``,
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		``,
		`data: {"type":"response.completed","response":{"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"text/event-stream"}}}

	usage, err := OaiResponsesStreamHandler(c, &relaycommon.RelayInfo{OriginModelName: "gpt-5.6-sol", IsStream: true}, resp)
	require.Nil(t, err)
	require.Equal(t, 15, usage.TotalTokens)
	output := recorder.Body.String()
	created := strings.Index(output, `event: response.created`)
	delta := strings.Index(output, `event: response.output_text.delta`)
	completed := strings.Index(output, `event: response.completed`)
	require.GreaterOrEqual(t, created, 0)
	require.Greater(t, delta, created)
	require.Greater(t, completed, delta)
	require.True(t, c.GetBool(string(constant.ContextKeyStreamContentDelivered)))
	require.Equal(t, gatewaystream.AttemptStageCompleted, gatewaystream.AttemptStageFromContext(c))
}

func TestOaiResponsesStreamHandlerFlushesRemoteCompactionOutput(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_compact"}}`,
		``,
		`data: {"type":"response.output_item.added","item":{"id":"ctc_123","type":"compaction","status":"completed"}}`,
		``,
		`data: {"type":"response.output_item.done","item":{"id":"ctc_123","type":"compaction","status":"completed"}}`,
		``,
		`data: {"type":"response.completed","response":{"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"text/event-stream"}}}

	usage, err := OaiResponsesStreamHandler(c, &relaycommon.RelayInfo{OriginModelName: "gpt-5.6-sol", IsStream: true}, resp)

	require.Nil(t, err)
	require.Equal(t, 15, usage.TotalTokens)
	output := recorder.Body.String()
	created := strings.Index(output, `event: response.created`)
	compaction := strings.Index(output, `event: response.output_item.added`)
	completed := strings.Index(output, `event: response.completed`)
	require.GreaterOrEqual(t, created, 0)
	require.Greater(t, compaction, created)
	require.Greater(t, completed, compaction)
	require.True(t, c.GetBool(string(constant.ContextKeyStreamContentDelivered)))
}

func TestOaiResponsesStreamHandlerDoesNotFailWhenLifecycleBufferOverflows(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	var body strings.Builder
	body.WriteString(`data: {"type":"response.created","response":{"id":"resp_123"}}` + "\n\n")
	for index := 0; index < responsesPreOutputEventLimit+8; index++ {
		body.WriteString(`data: {"type":"response.in_progress","response":{"id":"resp_123"}}` + "\n\n")
	}
	body.WriteString(`data: {"type":"response.output_text.delta","delta":"hello"}` + "\n\n")
	body.WriteString(`data: {"type":"response.completed","response":{"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}` + "\n\n")
	body.WriteString("data: [DONE]\n\n")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body.String())), Header: http.Header{"Content-Type": []string{"text/event-stream"}}}

	usage, err := OaiResponsesStreamHandler(c, &relaycommon.RelayInfo{OriginModelName: "gpt-5.6-sol", IsStream: true}, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, 15, usage.TotalTokens)
	require.Contains(t, recorder.Body.String(), `event: response.created`)
	require.Contains(t, recorder.Body.String(), `event: response.output_text.delta`)
	lifecycle, ok := c.Get("responses_stream_lifecycle")
	require.True(t, ok)
	require.Greater(t, lifecycle.(map[string]interface{})["pre_output_events_dropped"].(int), 0)
}

func TestOaiResponsesStreamHandlerSucceedsAfterResponseCompleted(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		``,
		`data: {"type":"response.completed","response":{"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15,"input_tokens_details":{"cached_tokens":8,"cached_creation_tokens":4}}}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-5.5",
		},
		IsStream: true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.Equal(t, &dto.Usage{
		PromptTokens:     12,
		CompletionTokens: 3,
		TotalTokens:      15,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:         8,
			CachedCreationTokens: 4,
		},
	}, usage)
	require.Equal(t, "hello", info.ConversationResponseText)
}

func TestOaiResponsesStreamHandlerRejectsEmptyCompletedEvent(t *testing.T) {
	setResponsesTestStreamingTimeout(t)
	body := "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n"
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"text/event-stream"}}}

	usage, err := OaiResponsesStreamHandler(c, &relaycommon.RelayInfo{OriginModelName: "gpt-5.6-sol", IsStream: true}, resp)
	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, http.StatusBadGateway, err.StatusCode)
	require.False(t, types.IsSkipRetryError(err))
	require.Empty(t, recorder.Body.String())
}

func TestOaiResponsesStreamHandlerForwardsTerminalFailureAfterOutput(t *testing.T) {
	setResponsesTestStreamingTimeout(t)
	body := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"partial"}`,
		``,
		`data: {"type":"response.failed","response":{"status":"failed","error":{"type":"server_error","message":"upstream failed"}}}`,
		``,
	}, "\n")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"text/event-stream"}}}

	usage, err := OaiResponsesStreamHandler(c, &relaycommon.RelayInfo{OriginModelName: "gpt-5.6-sol", IsStream: true}, resp)
	require.Nil(t, usage)
	require.NotNil(t, err)
	require.True(t, types.IsSkipRetryError(err))
	require.Contains(t, recorder.Body.String(), "event: response.failed")
	require.Contains(t, recorder.Body.String(), "upstream failed")
}

func TestOaiResponsesStreamHandlerAddsFailureWhenPartialStreamCloses(t *testing.T) {
	setResponsesTestStreamingTimeout(t)
	body := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"text/event-stream"}}}

	_, err := OaiResponsesStreamHandler(c, &relaycommon.RelayInfo{OriginModelName: "gpt-5.6-sol", IsStream: true}, resp)
	require.NotNil(t, err)
	require.True(t, types.IsSkipRetryError(err))
	require.Contains(t, recorder.Body.String(), "event: response.failed")
	require.Contains(t, recorder.Body.String(), "closed before response.completed")
}

func setResponsesTestStreamingTimeout(t *testing.T) {
	t.Helper()
	previous := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = previous })
}

func TestIsResponsesTextDelta(t *testing.T) {
	require.True(t, isResponsesTextDelta(dto.ResponsesStreamResponse{
		Type:  "response.output_text.delta",
		Delta: "text",
	}))
	require.False(t, isResponsesTextDelta(dto.ResponsesStreamResponse{
		Type:  "response.reasoning.delta",
		Delta: "reasoning",
	}))
	require.False(t, isResponsesTextDelta(dto.ResponsesStreamResponse{
		Type: "response.output_text.delta",
	}))
}

func TestHasResponsesStreamContentRecognizesRemoteCompactionOutput(t *testing.T) {
	require.True(t, hasResponsesStreamContent(dto.ResponsesStreamResponse{
		Type: dto.ResponsesOutputTypeItemAdded,
		Item: &dto.ResponsesOutput{Type: "compaction"},
	}))
	require.False(t, hasResponsesStreamContent(dto.ResponsesStreamResponse{
		Type: dto.ResponsesOutputTypeItemDone,
		Item: &dto.ResponsesOutput{Type: "compaction"},
	}))
}

func TestIsPaceableResponsesTextDelta(t *testing.T) {
	testCases := []struct {
		name string
		resp dto.ResponsesStreamResponse
		want bool
	}{
		{
			name: "output text",
			resp: dto.ResponsesStreamResponse{Type: "response.output_text.delta", Delta: "hello"},
			want: true,
		},
		{
			name: "reasoning summary",
			resp: dto.ResponsesStreamResponse{Type: "response.reasoning_summary_text.delta", Delta: "summary"},
			want: true,
		},
		{
			name: "function arguments",
			resp: dto.ResponsesStreamResponse{Type: "response.function_call_arguments.delta", Delta: "{}"},
			want: false,
		},
		{
			name: "completed event",
			resp: dto.ResponsesStreamResponse{Type: "response.completed", Delta: "text"},
			want: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.want, isPaceableResponsesTextDelta(testCase.resp))
		})
	}
}

func TestOaiResponsesHandlerPreservesCacheWriteTokens(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(
			`{"usage":{"input_tokens":120,"output_tokens":30,"total_tokens":150,"input_tokens_details":{"cached_tokens":80,"cached_creation_tokens":20}}}`,
		)),
		Header: http.Header{"Content-Type": []string{"application/json"}},
	}
	info := &relaycommon.RelayInfo{}

	usage, err := OaiResponsesHandler(c, info, resp)

	require.Nil(t, err)
	require.Equal(t, &dto.Usage{
		PromptTokens:     120,
		CompletionTokens: 30,
		TotalTokens:      150,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:         80,
			CachedCreationTokens: 20,
		},
	}, usage)
}

func TestOaiResponsesHandlerNormalizesCompatibleCacheWriteFields(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	for _, fieldName := range []string{
		"cache_creation_tokens",
		"cache_creation_input_tokens",
		"cache_write_tokens",
		"cache_write_input_tokens",
	} {
		t.Run(fieldName, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			body := `{"usage":{"input_tokens":120,"output_tokens":30,"total_tokens":150,"input_tokens_details":{"` + fieldName + `":20}}}`
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
			}

			usage, err := OaiResponsesHandler(c, &relaycommon.RelayInfo{}, resp)

			require.Nil(t, err)
			require.Equal(t, 20, usage.PromptTokensDetails.CachedCreationTokens)
		})
	}
}

func TestOaiResponsesToChatStreamHandlerPreservesCacheWriteTokens(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`data: {"type":"response.completed","response":{"usage":{"input_tokens":120,"output_tokens":30,"total_tokens":150,"input_tokens_details":{"cached_tokens":80,"cached_creation_tokens":20}}}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"text/event-stream"}}}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.5"}, RelayFormat: types.RelayFormatOpenAI, IsStream: true}

	usage, err := OaiResponsesToChatStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.Equal(t, 20, usage.PromptTokensDetails.CachedCreationTokens)
}
