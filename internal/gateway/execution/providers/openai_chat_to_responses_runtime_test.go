package providers

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

func TestChatToResponsesRuntimePreservesUsageAndRejectsPrematureEOF(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	for _, complete := range []bool{true, false} {
		t.Run(map[bool]string{true: "complete", false: "premature_eof"}[complete], func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "test"}, RelayFormat: types.RelayFormatOpenAIResponses, IsStream: true}
			info.SetEstimatePromptTokens(11)
			body := "data: {\"id\":\"chatcmpl-test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\n"
			if complete {
				body += "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"length\"}],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":9,\"total_tokens\":20,\"completion_tokens_details\":{\"reasoning_tokens\":7}}}\n\ndata: [DONE]\n\n"
			}
			resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
			usage, err := OaiChatToResponsesStreamHandler(c, info, resp, &dto.OpenAIResponsesRequest{Model: "test"}, nil)
			require.Contains(t, recorder.Body.String(), "response.output_text.delta")
			require.Equal(t, "hello", info.ConversationResponseText)
			if complete {
				require.Nil(t, err)
				require.Equal(t, 20, usage.TotalTokens)
				require.Equal(t, 7, usage.OutputTokensDetails.ReasoningTokens)
				require.Contains(t, recorder.Body.String(), "response.incomplete")
				require.Contains(t, recorder.Body.String(), `"reason":"max_output_tokens"`)
			} else {
				require.NotNil(t, err)
				require.NotContains(t, recorder.Body.String(), "response.completed")
				require.NotNil(t, usage)
				require.Equal(t, 11, usage.PromptTokens)
			}
		})
	}
}
