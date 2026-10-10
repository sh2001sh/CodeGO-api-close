package openai

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
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestChatStreamTimingObservesReasoningAndTextAfterRole(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	start := time.Now().Add(-time.Second)
	info := &relaycommon.RelayInfo{
		StartTime: start, IsStream: true, RelayFormat: types.RelayFormatOpenAI,
		OriginModelName: "gpt-5.6-sol", FirstByteTrace: relaycommon.NewFirstByteTrace(start),
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"},
	}
	body := strings.Join([]string{
		`data: {"choices":[{"delta":{"role":"assistant"}}]}`,
		`data: {"choices":[{"delta":{"reasoning_content":"thinking"}}]}`,
		`data: {"choices":[{"delta":{"content":"answer"}}]}`,
		`data: {"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}`,
		`data: [DONE]`, "",
	}, "\n")
	usage, apiErr := OaiStreamHandler(ctx, info, &http.Response{Body: io.NopCloser(strings.NewReader(body))})
	require.Nil(t, apiErr)
	require.Equal(t, 3, usage.CompletionTokens)
	require.True(t, info.HasSemanticResponse())
	trace := info.FirstByteTrace.Snapshot()
	require.EqualValues(t, 0, trace["first_semantic_is_text"])
	require.Contains(t, trace, "total_text_ms")
	require.Contains(t, trace, "e2e_first_text_ms")
}

func TestResponsesStreamTimingRecognizesTextInMessageItemAfterReasoning(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	start := time.Now().Add(-time.Second)
	info := &relaycommon.RelayInfo{
		StartTime: start, IsStream: true, RelayFormat: types.RelayFormatOpenAIResponses,
		OriginModelName: "gpt-5.6-sol", FirstByteTrace: relaycommon.NewFirstByteTrace(start),
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"},
	}
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1"}}`,
		`data: {"type":"response.reasoning_summary_text.delta","delta":"thinking"}`,
		`data: {"type":"response.output_item.added","item":{"type":"message","content":[{"type":"output_text","text":"answer"}]}}`,
		`data: {"type":"response.completed","response":{"usage":{"input_tokens":10,"output_tokens":3,"total_tokens":13}}}`, "",
	}, "\n")
	usage, apiErr := OaiResponsesStreamHandler(ctx, info, &http.Response{Body: io.NopCloser(strings.NewReader(body))})
	require.Nil(t, apiErr)
	require.Equal(t, 3, usage.CompletionTokens)
	require.True(t, info.HasSemanticResponse())
	trace := info.FirstByteTrace.Snapshot()
	require.EqualValues(t, 0, trace["first_semantic_is_text"])
	require.Contains(t, trace, "total_text_ms")
	require.Contains(t, trace, "e2e_first_text_ms")
}
