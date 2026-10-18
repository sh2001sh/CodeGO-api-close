package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	"github.com/sh2001sh/new-api/internal/platform/tokenx"
	"github.com/stretchr/testify/require"
)

type disconnectingResponsesWriter struct {
	*httptest.ResponseRecorder
	cancel     context.CancelFunc
	textEvents int
}

func (w *disconnectingResponsesWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), `"type":"response.output_text.delta"`) {
		w.textEvents++
		if w.textEvents == 2 {
			w.cancel()
			return 0, io.ErrClosedPipe
		}
	}
	return w.ResponseRecorder.Write(p)
}

func (w *disconnectingResponsesWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func TestResponsesDisconnectDuringSplitDeltaPreservesObservedOutput(t *testing.T) {
	tokenx.InitTokenEncoders()
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &disconnectingResponsesWriter{ResponseRecorder: httptest.NewRecorder(), cancel: cancel}
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
	c.Set(string(constant.ContextKeyUpstreamRequestAccepted), true)
	text := strings.Repeat("observed output ", 100)
	body := `data: {"type":"response.output_text.delta","delta":"` + text + `"}` + "\n\n"
	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "gpt-6.1-sol"},
		OriginModelName: "gpt-6.1-sol", IsStream: true,
		StreamPacer: relaycommon.NewStreamPacer("gpt-6.1-sol"),
	}
	info.SetEstimatePromptTokens(123)
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"text/event-stream"}}}
	usage, err := OaiResponsesStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.Positive(t, info.StreamPacer.OutputTokens())
	require.Equal(t, text, info.ConversationResponseText)
	require.NotNil(t, usage)
	require.Positive(t, usage.CompletionTokens)
	require.Equal(t, 123, usage.PromptTokens)
	require.Equal(t, "local_completion_fallback", usage.UsageSource)
}

func TestResponsesInterruptedNonTextOutputRemainsAvailableForSettlement(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	for _, event := range []string{"response.reasoning_summary_text.delta", "response.refusal.delta", "response.function_call_arguments.delta"} {
		t.Run(event, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			body := `data: {"type":"` + event + `","delta":"observed"}` + "\n\n"
			info := &relaycommon.RelayInfo{IsStream: true}
			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"text/event-stream"}}}
			usage, err := OaiResponsesStreamHandler(c, info, resp)
			require.Error(t, err)
			require.Nil(t, usage)
			require.Equal(t, "observed", info.ConversationResponseText)
		})
	}
}
