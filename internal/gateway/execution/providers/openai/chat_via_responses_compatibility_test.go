package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestResponsesToChatRuntimePreservesMixedTextToolsAndIncomplete(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	frames := []string{
		`{"type":"response.output_text.delta","delta":"running tool"}`,
		`{"type":"response.output_item.added","item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"lookup","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{}"}`,
		`{"type":"response.incomplete","response":{"incomplete_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":11,"output_tokens":9,"total_tokens":20,"output_tokens_details":{"reasoning_tokens":7}}}}`,
	}
	var body strings.Builder
	for _, frame := range frames {
		body.WriteString("data: " + frame + "\n\n")
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body.String()))}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "test"}, RelayFormat: types.RelayFormatOpenAI, IsStream: true}
	usage, err := OaiResponsesToChatStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.Equal(t, 20, usage.TotalTokens)
	require.Equal(t, 7, usage.CompletionTokenDetails.ReasoningTokens)
	require.Contains(t, recorder.Body.String(), `"content":"running tool"`)
	require.Contains(t, recorder.Body.String(), `"id":"call_1"`)
	require.Contains(t, recorder.Body.String(), `"finish_reason":"length"`)
}

func TestResponsesToChatRuntimeRejectsTruncationAndTopLevelErrors(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	for _, frame := range []string{`{"type":"response.output_text.delta","delta":"partial"}`, `{"type":"error","error":{"type":"invalid_request_error","message":"test error","code":"test_code"}}`} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + frame + "\n\n"))}
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "test"}, RelayFormat: types.RelayFormatOpenAI, IsStream: true}
		_, err := OaiResponsesToChatStreamHandler(c, info, resp)
		require.NotNil(t, err)
		require.NotContains(t, recorder.Body.String(), `"finish_reason":"stop"`)
	}
}
