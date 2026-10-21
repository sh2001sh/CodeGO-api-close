package providers

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
	"github.com/sh2001sh/new-api/internal/platform/tokenx"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

type cancelBridgeWriter struct {
	*httptest.ResponseRecorder
	cancel context.CancelFunc
}

func (w *cancelBridgeWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseRecorder.Write(p)
	if strings.Contains(string(p), "reasoning") {
		w.cancel()
	}
	return n, err
}

func (w *cancelBridgeWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }

func TestRuntimeBridgesRecoverUsageAfterReasoningDisconnect(t *testing.T) {
	tokenx.InitTokenEncoders()
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	for _, chatUpstream := range []bool{false, true} {
		t.Run(map[bool]string{true: "chat_to_responses", false: "responses_to_chat"}[chatUpstream], func(t *testing.T) {
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			writer := &cancelBridgeWriter{httptest.NewRecorder(), cancel}
			c, _ := gin.CreateTestContext(writer)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(parent)
			c.Set(string(constant.ContextKeyUpstreamRequestAccepted), true)
			recovery := relaycommon.NewStreamUsageRecovery(parent, time.Second, time.Second)
			defer recovery.Close()
			recovery.Accept()
			reader, upstream := io.Pipe()
			defer upstream.Close()
			go func() {
				defer upstream.Close()
				first := `{"type":"response.reasoning_text.delta","delta":"reasoning"}`
				last := `{"type":"response.completed","response":{"usage":{"input_tokens":123,"output_tokens":900,"total_tokens":1023,"output_tokens_details":{"reasoning_tokens":850}}}}`
				if chatUpstream {
					first = `{"choices":[{"delta":{"reasoning_content":"reasoning"}}]}`
					last = `{"choices":[],"usage":{"prompt_tokens":123,"completion_tokens":900,"total_tokens":1023,"completion_tokens_details":{"reasoning_tokens":850}}}`
				}
				if _, err := io.WriteString(upstream, "data: "+first+"\n\n"); err != nil {
					return
				}
				select {
				case <-parent.Done():
				case <-recovery.Context().Done():
					return
				}
				_, _ = io.WriteString(upstream, "data: "+last+"\n\n")
			}()
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.5"}, IsStream: true, DisablePing: true, RelayFormat: types.RelayFormatOpenAI, StreamUsageRecovery: recovery}
			resp := &http.Response{StatusCode: 200, Body: reader, Header: http.Header{"Content-Type": []string{"text/event-stream"}}}
			var usage *dto.Usage
			var err *types.NewAPIError
			if chatUpstream {
				usage, err = OaiChatToResponsesStreamHandler(c, info, resp, &dto.OpenAIResponsesRequest{Model: "gpt-5.5"}, nil)
			} else {
				usage, err = OaiResponsesToChatStreamHandler(c, info, resp)
			}
			require.Nil(t, err)
			require.Equal(t, 900, usage.CompletionTokens)
			require.Equal(t, 850, usage.CompletionTokenDetails.ReasoningTokens)
			require.Equal(t, "upstream_final_usage_recovered", usage.UsageSource)
		})
	}
}
