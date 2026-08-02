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
