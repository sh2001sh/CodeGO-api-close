package openai

import (
	"context"
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
	"github.com/sh2001sh/new-api/internal/gateway/execution/providers/synchttp"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	"github.com/sh2001sh/new-api/internal/platform/tokenx"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

type disconnectOnReasoningWriter struct {
	*httptest.ResponseRecorder
	once       sync.Once
	disconnect func()
	brokenPipe bool
	closed     bool
	lateWrites int
}

func (w *disconnectOnReasoningWriter) Write(p []byte) (int, error) {
	if w.closed {
		w.lateWrites++
		return 0, io.ErrClosedPipe
	}
	if strings.Contains(string(p), "reasoning") {
		w.once.Do(w.disconnect)
		w.closed = true
		if w.brokenPipe {
			return 0, io.ErrClosedPipe
		}
	}
	return w.ResponseRecorder.Write(p)
}

func (w *disconnectOnReasoningWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func TestStreamRecoversFinalUpstreamUsageAfterDownstreamDisconnect(t *testing.T) {
	tokenx.InitTokenEncoders()
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	for _, protocol := range []string{"responses", "responses_to_chat", "chat"} {
		for _, brokenPipe := range []bool{false, true} {
			name := protocol + "/cancel"
			if brokenPipe {
				name = protocol + "/broken_pipe"
			}
			t.Run(name, func(t *testing.T) {
				parent, cancel := context.WithCancel(context.Background())
				defer cancel()
				disconnected := make(chan struct{})
				upstreamFinished := make(chan struct{})
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/event-stream")
					first := `{"type":"response.reasoning_text.delta","delta":"billable reasoning"}`
					if protocol == "chat" {
						first = `{"id":"chat_1","choices":[{"delta":{"reasoning_content":"billable reasoning"}}]}`
					}
					_, _ = io.WriteString(w, "data: "+first+"\n\n")
					w.(http.Flusher).Flush()
					select {
					case <-disconnected:
					case <-r.Context().Done():
						return
					}
					// Generation continues after the downstream has gone away.
					last := `data: {"type":"response.output_text.delta","delta":"answer after disconnect"}` + "\n\n" +
						`data: {"type":"response.completed","response":{"usage":{"input_tokens":123,"output_tokens":900,"total_tokens":1023,"input_tokens_details":{"cached_tokens":20},"output_tokens_details":{"reasoning_tokens":850}}}}` + "\n\n"
					if protocol == "chat" {
						last = `data: {"choices":[{"delta":{"content":"answer after disconnect"}}]}` + "\n\n" +
							`data: {"choices":[],"usage":{"prompt_tokens":123,"completion_tokens":900,"total_tokens":1023,"prompt_tokens_details":{"cached_tokens":20},"completion_tokens_details":{"reasoning_tokens":850}}}` + "\n\ndata: [DONE]\n\n"
					}
					_, _ = io.WriteString(w, last)
					w.(http.Flusher).Flush()
					close(upstreamFinished)
				}))
				defer server.Close()
				writer := &disconnectOnReasoningWriter{ResponseRecorder: httptest.NewRecorder(), brokenPipe: brokenPipe}
				writer.disconnect = func() {
					if !brokenPipe {
						cancel()
					}
					close(disconnected)
				}
				c, _ := gin.CreateTestContext(writer)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(parent)
				info := &relaycommon.RelayInfo{
					ChannelMeta:     &relaycommon.ChannelMeta{ApiType: constant.APITypeOpenAI, UpstreamModelName: "gpt-5.5"},
					OriginModelName: "gpt-5.5", IsStream: true, DisablePing: true,
					RelayMode: gatewaycontract.RelayModeResponses, RelayFormat: types.RelayFormatOpenAI,
				}
				info.SetEstimatePromptTokens(999) // Final provider input must win over this estimate.
				if protocol == "chat" {
					info.RelayMode = gatewaycontract.RelayModeChatCompletions
				}
				req, err := http.NewRequestWithContext(parent, http.MethodPost, server.URL, nil)
				require.NoError(t, err)
				resp, err := synchttp.DoRequest(c, req, info)
				require.NoError(t, err)
				defer resp.Body.Close()
				require.NotNil(t, info.StreamUsageRecovery)
				handler := OaiResponsesStreamHandler
				if protocol == "responses_to_chat" {
					handler = OaiResponsesToChatStreamHandler
				} else if protocol == "chat" {
					handler = OaiStreamHandler
				}
				usage, apiErr := handler(c, info, resp)
				require.Nil(t, apiErr)
				require.NotNil(t, usage)
				require.Equal(t, 123, usage.PromptTokens)
				require.Equal(t, 900, usage.CompletionTokens, "output already includes reasoning; never add it twice")
				require.Equal(t, 1023, usage.TotalTokens)
				require.Equal(t, 850, usage.CompletionTokenDetails.ReasoningTokens)
				require.Equal(t, 20, usage.PromptTokensDetails.CachedTokens)
				require.Equal(t, "upstream_final_usage_recovered", usage.UsageSource)
				require.False(t, c.GetBool("usage_reconciliation_required"))
				require.NotContains(t, writer.Body.String(), "answer after disconnect")
				require.Zero(t, writer.lateWrites, "never write downstream during recovery")
				select {
				case <-upstreamFinished:
				case <-time.After(time.Second):
					t.Fatal("upstream did not finish")
				}
				t.Logf("upstream_output=900 gateway_output=%d reasoning=%d cached=%d source=%s", usage.CompletionTokens, usage.CompletionTokenDetails.ReasoningTokens, usage.PromptTokensDetails.CachedTokens, usage.UsageSource)
			})
		}
	}
}

func TestResponsesRecoveryTimeoutPreservesObservedReasoning(t *testing.T) {
	tokenx.InitTokenEncoders()
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	recovery := relaycommon.NewStreamUsageRecovery(parent, time.Second, 30*time.Millisecond)
	defer recovery.Close()
	recovery.Accept()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: "+`{"type":"response.reasoning_text.delta","delta":"billable reasoning"}`+"\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done() // Never sends final usage.
	}))
	defer server.Close()
	req, err := http.NewRequestWithContext(recovery.Context(), http.MethodPost, server.URL, nil)
	require.NoError(t, err)
	resp, err := server.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	writer := &disconnectOnReasoningWriter{ResponseRecorder: httptest.NewRecorder(), disconnect: cancel}
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(parent)
	c.Set(string(constant.ContextKeyUpstreamRequestAccepted), true)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.5"}, IsStream: true, DisablePing: true, StreamUsageRecovery: recovery}
	info.SetEstimatePromptTokens(123)
	started := time.Now()
	usage, apiErr := OaiResponsesStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.Less(t, time.Since(started), time.Second)
	require.Positive(t, usage.CompletionTokens)
	require.Equal(t, 123, usage.PromptTokens)
	require.Equal(t, "local_incomplete_usage", usage.UsageSource)
	require.True(t, c.GetBool("usage_reconciliation_required"))
	_, _, metadata := recovery.Snapshot()
	require.Equal(t, "recovery_timeout", metadata["end_reason"])
}

func TestStreamRecoveryDoesNotChargeProviderFailureAfterDisconnect(t *testing.T) {
	for _, protocol := range []string{"responses", "responses_to_chat", "chat"} {
		t.Run(protocol, func(t *testing.T) {
			setResponsesTestStreamingTimeout(t)
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			disconnected := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				first := `{"type":"response.reasoning_text.delta","delta":"reasoning"}`
				failure := `{"type":"response.failed","response":{"status":"failed","error":{"message":"provider unavailable"},"usage":{"input_tokens":90000,"output_tokens":0}}}`
				if protocol == "chat" {
					first = `{"choices":[{"delta":{"reasoning_content":"reasoning"}}]}`
					failure = `{"error":{"message":"provider unavailable","code":"overloaded"}}`
				}
				_, _ = io.WriteString(w, "data: "+first+"\n\n")
				w.(http.Flusher).Flush()
				select {
				case <-disconnected:
				case <-r.Context().Done():
					return
				}
				_, _ = io.WriteString(w, "data: "+failure+"\n\n")
				w.(http.Flusher).Flush()
			}))
			defer server.Close()
			writer := &disconnectOnReasoningWriter{ResponseRecorder: httptest.NewRecorder()}
			writer.disconnect = func() { cancel(); close(disconnected) }
			c, _ := gin.CreateTestContext(writer)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(parent)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.5"}, IsStream: true, DisablePing: true, RelayMode: gatewaycontract.RelayModeResponses, RelayFormat: types.RelayFormatOpenAI}
			info.SetEstimatePromptTokens(90000)
			req, err := http.NewRequestWithContext(parent, http.MethodPost, server.URL, nil)
			require.NoError(t, err)
			resp, err := synchttp.DoRequest(c, req, info)
			require.NoError(t, err)
			defer resp.Body.Close()
			handler := OaiResponsesStreamHandler
			if protocol == "responses_to_chat" {
				handler = OaiResponsesToChatStreamHandler
			}
			if protocol == "chat" {
				handler = OaiStreamHandler
			}
			usage, apiErr := handler(c, info, resp)
			require.Nil(t, usage)
			require.NotNil(t, apiErr)
			require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
			require.True(t, c.GetBool(string(constant.ContextKeyUpstreamTerminalError)))
			require.Zero(t, writer.lateWrites)
			t.Log("provider_failure=true usage=nil refund_required=true")
		})
	}
}

func TestResponsesBridgeForwardsRawReasoningWithExactUsage(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	body := "data: " + `{"type":"response.reasoning_text.delta","delta":"billable reasoning"}` + "\n\n" +
		"data: " + `{"type":"response.completed","response":{"usage":{"input_tokens":123,"output_tokens":9,"total_tokens":132,"output_tokens_details":{"reasoning_tokens":9}}}}` + "\n\n"
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.5"}, RelayFormat: types.RelayFormatOpenAI, IsStream: true, DisablePing: true}
	resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	usage, err := OaiResponsesToChatStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.Equal(t, 9, usage.CompletionTokens)
	require.Equal(t, 9, usage.CompletionTokenDetails.ReasoningTokens)
	require.Contains(t, recorder.Body.String(), `"reasoning_content":"billable reasoning"`)
}

func TestResponsesBridgeKeepsActiveRawReasoningPastAdaptiveDeadline(t *testing.T) {
	oldIdle, oldInitial, oldProgress := constant.StreamingTimeout, constant.StreamingAdaptiveInitialTimeout, constant.StreamingAdaptiveProgressTimeout
	constant.StreamingTimeout, constant.StreamingAdaptiveInitialTimeout, constant.StreamingAdaptiveProgressTimeout = 5, 1, 1
	t.Cleanup(func() {
		constant.StreamingTimeout, constant.StreamingAdaptiveInitialTimeout, constant.StreamingAdaptiveProgressTimeout = oldIdle, oldInitial, oldProgress
	})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	relaycommon.MarkLongContextRequest(c, "gpt-5.5", relaycommon.LongContextPromptTokenThreshold)
	reader, writer := io.Pipe()
	defer writer.Close()
	go func() {
		defer writer.Close()
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for i := 0; i < 6; i++ {
			<-ticker.C
			if _, err := io.WriteString(writer, "data: "+`{"type":"response.reasoning_text.delta","delta":"thinking"}`+"\n\n"); err != nil {
				return
			}
		}
		_, _ = io.WriteString(writer, "data: "+`{"type":"response.completed","response":{"usage":{"input_tokens":123,"output_tokens":9,"total_tokens":132}}}`+"\n\n")
	}()
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.5"}, OriginModelName: "gpt-5.5", RelayFormat: types.RelayFormatOpenAI, IsStream: true, DisablePing: true}
	resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: reader}
	usage, err := OaiResponsesToChatStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.Equal(t, 9, usage.CompletionTokens)
	require.Empty(t, relaycommon.LocalStreamTimeoutReason(c))
}
