package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func newOpenAIStreamTestContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return c, recorder
}

func TestOaiStreamHandlerRejectsEmptyUpstreamStream(t *testing.T) {
	c, recorder := newOpenAIStreamTestContext(t)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"},
		OriginModelName: "gpt-5.6-sol",
		RelayMode:       gatewaycontract.RelayModeChatCompletions,
		IsStream:        true,
	}
	info.SetEstimatePromptTokens(100)

	usage, err := OaiStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, http.StatusBadGateway, err.StatusCode)
	require.Equal(t, types.ErrorCodeBadResponseBody, err.GetErrorCode())
	require.Empty(t, recorder.Body.String())
}

func TestOaiStreamHandlerBackfillsOutputTokensWhenUpstreamUsageReportsZero(t *testing.T) {
	c, _ := newOpenAIStreamTestContext(t)
	body := strings.Join([]string{
		`data: {"id":"chatcmpl-test","choices":[{"delta":{"content":"hello world"}}]}`,
		``,
		`data: {"id":"chatcmpl-test","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":0,"total_tokens":10}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"},
		OriginModelName: "gpt-5.6-sol",
		RelayMode:       gatewaycontract.RelayModeChatCompletions,
		IsStream:        true,
	}

	usage, err := OaiStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, 10, usage.PromptTokens)
	require.NotEmpty(t, info.ConversationResponseText)
	require.Positive(t, usage.CompletionTokens)
	require.Equal(t, usage.PromptTokens+usage.CompletionTokens, usage.TotalTokens)
}

func TestOaiStreamHandlerAllowsUpstreamInputOnlyUsage(t *testing.T) {
	c, _ := newOpenAIStreamTestContext(t)
	body := strings.Join([]string{
		`data: {"id":"chatcmpl-test","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":0,"total_tokens":10}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"},
		OriginModelName: "gpt-5.6-sol",
		RelayMode:       gatewaycontract.RelayModeChatCompletions,
		IsStream:        true,
	}

	usage, err := OaiStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, 10, usage.PromptTokens)
	require.Zero(t, usage.CompletionTokens)
	require.Equal(t, 10, usage.TotalTokens)
}
