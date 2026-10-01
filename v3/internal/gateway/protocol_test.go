package gateway_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/anthropic"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/bridge"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/gemini"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/openai"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/responses"
)

func nativeHarness(t *testing.T, provider gateway.Provider, id, payload string) *harness {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(upstream.Close)
	planner := &fakePlanner{targets: []gateway.Target{{ChannelID: 1, CredentialID: 1, Provider: id, BaseURL: upstream.URL}}}
	settler := newSettler()
	g, err := gateway.New(gateway.Deps{Authorizer: fakeAuth{}, Planner: planner, Settler: settler, Providers: map[string]gateway.Provider{id: provider}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	g.Register(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return &harness{t: t, gw: server, planner: planner, settler: settler}
}

func TestNativeProtocolsPreserveMarkersAndUsage(t *testing.T) {
	cases := []struct {
		name, path, body, wire, marker string
		provider                       gateway.Provider
	}{
		{"anthropic", "/v1/messages", `{"model":"claude","stream":true,"max_tokens":10,"messages":[]}`,
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg1\",\"usage\":{\"input_tokens\":42,\"output_tokens\":0}}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\nevent: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n", "event: message_stop", anthropic.Provider{}},
		{"responses", "/v1/responses", `{"model":"gpt","stream":true,"input":"hello"}`,
			"event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"r1\",\"status\":\"in_progress\"}}\n\nevent: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"r1\",\"status\":\"completed\",\"usage\":{\"input_tokens\":42,\"output_tokens\":5}}}\n\n", "event: response.completed", responses.Provider{}},
		{"gemini", "/v1beta/models/gemini:streamGenerateContent", `{"contents":[]}`,
			"data: {\"candidates\":[{\"index\":0,\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"hello\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":42,\"candidatesTokenCount\":5}}\n\n", "hello", gemini.Provider{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := nativeHarness(t, tc.provider, tc.name, tc.wire)
			r, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, h.gw.URL+tc.path, strings.NewReader(tc.body))
			r.Header.Set("X-Api-Key", "sk-test")
			resp, err := http.DefaultClient.Do(r)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			out := h.outcome()
			if resp.StatusCode != 200 || !strings.Contains(string(body), tc.marker) || strings.Contains(string(body), "[DONE]") {
				t.Fatalf("status=%d body=%s", resp.StatusCode, body)
			}
			if out.Terminal != gateway.TerminalCompleted || !out.Charge || out.Usage.PromptTokens != 42 || out.Usage.CompletionTokens != 5 {
				t.Fatalf("outcome=%+v", out)
			}
		})
	}
}

func TestResponsesTerminalErrorKeepsActualUsageAndNativePayload(t *testing.T) {
	wire := "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\nevent: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"error\":{\"code\":\"overloaded\",\"message\":\"busy\"},\"usage\":{\"input_tokens\":42,\"output_tokens\":3}}}\n\n"
	h := nativeHarness(t, responses.Provider{}, responses.ID, wire)
	r, _ := http.NewRequest(http.MethodPost, h.gw.URL+"/v1/responses", strings.NewReader(`{"model":"gpt","stream":true,"input":"hello"}`))
	r.Header.Set("Authorization", "Bearer sk-test")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	out := h.outcome()
	if !strings.Contains(string(body), "event: response.failed") || out.Terminal != gateway.TerminalUpstreamErrorAfterOutput || out.Usage.Estimated || out.Usage.CompletionTokens != 3 {
		t.Fatalf("body=%s outcome=%+v", body, out)
	}
}

// Regression (protocol review): hidden native-tool accounting was dropped by
// relay, producing a zero completion estimate for Gemini tool-only streams.
func TestBridgedToolOnlyStreamPreservesBillingBytes(t *testing.T) {
	wire := "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call1\",\"type\":\"function\",\"function\":{\"name\":\"spell\",\"arguments\":\"{\\\"word\\\":\\\"hello\\\"}\"}}]}}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n"
	for _, tc := range []struct{ name, path, body string }{
		{"responses", "/v1/responses", `{"model":"gpt","stream":true,"input":"hello"}`},
		{"anthropic", "/v1/messages", `{"model":"gpt","stream":true,"max_tokens":10,"messages":[{"role":"user","content":"hello"}]}`},
		{"gemini", "/v1beta/models/gpt:streamGenerateContent", `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := nativeHarness(t, bridge.Provider{Chat: openai.Provider{}}, "bridge", wire)
			r, _ := http.NewRequest(http.MethodPost, h.gw.URL+tc.path, strings.NewReader(tc.body))
			r.Header.Set("Authorization", "Bearer sk-test")
			resp, err := http.DefaultClient.Do(r)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			out := h.outcome()
			if resp.StatusCode != 200 || out.Terminal != gateway.TerminalCompletedNoUsage || !out.Charge || out.Usage.CompletionTokens != 4 {
				t.Fatalf("body=%s outcome=%+v", body, out)
			}
		})
	}
}
