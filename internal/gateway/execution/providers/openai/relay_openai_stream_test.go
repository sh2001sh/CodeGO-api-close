package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

type notifiedStreamWriter struct {
	*httptest.ResponseRecorder
	flushed chan struct{}
	once    sync.Once
}

func (w *notifiedStreamWriter) Flush() {
	w.ResponseRecorder.Flush()
	w.once.Do(func() { close(w.flushed) })
}

func TestOaiStreamHandlerFlushesOutputBeforeNextFrame(t *testing.T) {
	for _, delta := range []string{
		`{"content":"first-output"}`,
		`{"reasoning_content":"first-output"}`,
		`{"tool_calls":[{"index":0,"id":"first-output","type":"function","function":{"name":"lookup","arguments":"{}"}}]}`,
	} {
		t.Run(delta, func(t *testing.T) {
			c, _ := newOpenAIStreamTestContext(t)
			writer := &notifiedStreamWriter{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan struct{})}
			c, _ = gin.CreateTestContext(writer)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			reader, upstream := io.Pipe()
			defer reader.Close()
			defer upstream.Close()
			info := &relaycommon.RelayInfo{
				ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "gpt-4.1"},
				OriginModelName: "gpt-4.1", RelayMode: gatewaycontract.RelayModeChatCompletions,
				IsStream: true, RelayFormat: types.RelayFormatOpenAI,
			}
			finished := make(chan struct{})
			var apiErr *types.NewAPIError
			go func() {
				defer close(finished)
				_, apiErr = OaiStreamHandler(c, info, &http.Response{StatusCode: 200, Body: reader, Header: http.Header{"Content-Type": {"text/event-stream"}}})
			}()
			_, err := io.WriteString(upstream, `data: {"id":"test","choices":[{"index":0,"delta":`+delta+`}]}`+"\n\n")
			require.NoError(t, err)
			// No next event or EOF is available: an implementation that buffers the
			// previous frame cannot flush until this assertion has already failed.
			select {
			case <-writer.flushed:
			case <-time.After(time.Second):
				t.Error("first output waited for the next upstream frame")
			}
			_, err = io.WriteString(upstream, "data: [DONE]\n\n")
			require.NoError(t, err)
			require.NoError(t, upstream.Close())
			<-finished
			require.Nil(t, apiErr)
			require.Equal(t, 1, strings.Count(writer.Body.String(), "first-output"), "finalization must not resend the output")
			require.Equal(t, 1, strings.Count(writer.Body.String(), "[DONE]"))
		})
	}
}

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

func TestOaiStreamHandlerRejectsUpstreamErrorsWithoutEstimatingUsage(t *testing.T) {
	for _, body := range []string{
		`data: {"error":{"type":"server_error","message":"provider unavailable"}}` + "\n\n",
		`data: {"error":{"message":"provider unavailable","code":"overloaded"}}` + "\n\n",
		`data: {"choices":[{"delta":{"role":"assistant"}}]}` + "\n\n",
		`data: {invalid` + "\n\n",
	} {
		t.Run(body, func(t *testing.T) {
			c, recorder := newOpenAIStreamTestContext(t)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"}, IsStream: true, RelayFormat: types.RelayFormatOpenAI}
			info.SetEstimatePromptTokens(90000)
			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": {"text/event-stream"}}}
			usage, apiErr := OaiStreamHandler(c, info, resp)
			require.Nil(t, usage)
			require.NotNil(t, apiErr)
			require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
			require.NotContains(t, recorder.Body.String(), "[DONE]")
		})
	}
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

func TestOaiStreamHandlerTranslatedSingleOutputIsNotRepeatedAtEOF(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatGemini} {
		t.Run(string(format), func(t *testing.T) {
			c, recorder := newOpenAIStreamTestContext(t)
			info := &relaycommon.RelayInfo{
				ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "gpt-4.1"},
				OriginModelName: "gpt-4.1", RelayMode: gatewaycontract.RelayModeChatCompletions,
				IsStream: true, RelayFormat: format, ClaudeConvertInfo: &relaycommon.ClaudeConvertInfo{},
			}
			body := "data: {\"id\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"unique-output\"}}]}\n\ndata: [DONE]\n\n"
			_, apiErr := OaiStreamHandler(c, info, &http.Response{Body: io.NopCloser(strings.NewReader(body))})
			require.Nil(t, apiErr)
			require.Equal(t, 1, strings.Count(recorder.Body.String(), "unique-output"))
			if format == types.RelayFormatClaude {
				require.True(t, info.ClaudeConvertInfo.Done)
			}
		})
	}
}
