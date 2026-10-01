package ali_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/ali"
	"github.com/tidwall/gjson"
)

func TestNativeMessagesModelSelectionAndEndpoint(t *testing.T) {
	body := []byte(`{"model":"client-alias","stream":true,"max_tokens":256,"system":"policy","messages":[{"role":"user","content":"hi"}],"tools":[{"name":"lookup","input_schema":{"type":"object"}}]}`)
	for _, base := range []string{"https://example.test/prefix?feature=one", "https://example.test/prefix/apps/anthropic?feature=one", "https://example.test/prefix/apps/anthropic/v1/?feature=one", "https://example.test/prefix/apps/anthropic/v1/messages?feature=one", "https://example.test/prefix/compatible-mode/v1?feature=one"} {
		req := &gateway.Request{Protocol: gateway.ProtocolAnthropic, Model: "client-alias", Body: body, Stream: true, ClientHeaders: map[string]string{"Authorization": "Bearer caller-secret", "X-Api-Key": "caller-secret"}}
		out, err := (ali.Provider{}).BuildRequest(context.Background(), req, gateway.Target{BaseURL: base, Secret: "channel-secret", UpstreamModel: " QWEN3-max ", Settings: map[string]any{"plugin": "plugin-name"}})
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(out.Body)
		if err != nil {
			t.Fatal(err)
		}
		if out.URL.Path != "/prefix/apps/anthropic/v1/messages" || out.URL.Query().Get("feature") != "one" {
			t.Fatalf("native endpoint changed: %s", out.URL)
		}
		if out.Header.Get("Authorization") != "Bearer channel-secret" || out.Header.Get("X-Api-Key") != "" || out.Header.Get("X-DashScope-Plugin") != "plugin-name" || out.Header.Get("X-DashScope-SSE") != "enable" || out.Header.Get("Anthropic-Version") != "2023-06-01" {
			t.Fatalf("unsafe/native headers: %v", out.Header)
		}
		if gjson.GetBytes(data, "model").Str != " QWEN3-max " || gjson.GetBytes(data, "tools.0.name").Str != "lookup" || gjson.GetBytes(data, "system").Str != "policy" || gjson.GetBytes(data, "stream_options").Exists() {
			t.Fatalf("native features changed: %s", data)
		}
		if gjson.GetBytes(body, "model").Str != "client-alias" {
			t.Fatal("original body mutated")
		}
	}
	for _, model := range []string{"deepseek-v4-pro", "KIMI-k2", "glm-5", "minimax-m2", "vendor/qwen-plus"} {
		out, err := (ali.Provider{}).BuildRequest(context.Background(), &gateway.Request{Protocol: gateway.ProtocolAnthropic, Model: model, Body: body}, gateway.Target{BaseURL: "https://example.test"})
		if err != nil || out.URL.Path != "/apps/anthropic/v1/messages" {
			t.Fatalf("v2 pattern lost for %s: %v %v", model, out, err)
		}
	}
}

func TestConfiguredMessagesPatternsAndLegacyChatFallback(t *testing.T) {
	body := []byte(`{"model":"client","max_tokens":256,"messages":[{"role":"user","content":"hi"}]}`)
	for _, tc := range []struct {
		patterns    []string
		model, path string
	}{
		{nil, "deepseek-v3", "/compatible-mode/v1/chat/completions"},
		{[]string{" custom ", ""}, "CUSTOM-v1", "/apps/anthropic/v1/messages"},
		{[]string{"custom"}, "qwen-plus", "/compatible-mode/v1/chat/completions"},
		{[]string{}, "qwen-plus", "/compatible-mode/v1/chat/completions"},
	} {
		out, err := (ali.Provider{AnthropicModelPatterns: tc.patterns}).BuildRequest(context.Background(), &gateway.Request{Protocol: gateway.ProtocolAnthropic, Model: "client", Body: body}, gateway.Target{BaseURL: "https://example.test/apps/anthropic/v1/messages", UpstreamModel: tc.model, Secret: "channel-secret"})
		if err != nil || out.URL.Path != tc.path {
			t.Fatalf("patterns=%v model=%s request=%v err=%v", tc.patterns, tc.model, out, err)
		}
		if out.Header.Get("Authorization") != "Bearer channel-secret" || out.Header.Get("X-Api-Key") != "" {
			t.Fatalf("wrong fallback credential: %v", out.Header)
		}
	}
}

func aliMessagesFixture(t *testing.T, provider ali.Provider, model, fixture string, stream bool) gateway.EventStream {
	t.Helper()
	req := &gateway.Request{Protocol: gateway.ProtocolAnthropic, Model: "friendly", Stream: stream, Body: []byte(`{"model":"friendly","stream":` + strconv.FormatBool(stream) + `,"max_tokens":256,"messages":[{"role":"user","content":"hi"}],"tools":[{"name":"lookup","input_schema":{"type":"object"}}]}`)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read HTTP fixture request: %v", err)
			w.WriteHeader(400)
			return
		}
		if r.Header.Get("Authorization") != "Bearer channel-secret" || r.Header.Get("X-Api-Key") != "" || r.Header.Get("X-DashScope-Plugin") != "plugin-name" || gjson.GetBytes(data, "model").Str != model {
			t.Errorf("bad actual HTTP request %v %s", r.Header, data)
		}
		if gjson.GetBytes(data, "stream").Bool() != stream {
			t.Errorf("actual HTTP stream flag changed: %s", data)
		}
		if strings.Contains(model, "qwen") && r.URL.Path != "/prefix/apps/anthropic/v1/messages" {
			t.Errorf("native fixture path: %s", r.URL.Path)
		}
		if stream {
			w.Header().Set("Content-Type", "text/event-stream")
		} else {
			w.Header().Set("Content-Type", "application/json")
		}
		_, _ = io.WriteString(w, fixture)
	}))
	t.Cleanup(server.Close)
	out, err := provider.BuildRequest(context.Background(), req, gateway.Target{BaseURL: server.URL + "/prefix", Secret: "channel-secret", UpstreamModel: model, Settings: map[string]any{"plugin": "plugin-name"}})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := server.Client().Do(out)
	if err != nil {
		t.Fatal(err)
	}
	s := provider.Decode(req, resp)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestNativeMessagesHTTPBodyToolsAndCachedUsage(t *testing.T) {
	fixture := `{"id":"msg1","type":"message","content":[{"type":"tool_use","id":"tool1","name":"lookup","input":{"q":"hi"}}],"stop_reason":"tool_use","usage":{"input_tokens":3,"output_tokens":4,"cache_read_input_tokens":5,"cache_creation_input_tokens":6,"cache_creation":{"ephemeral_1h_input_tokens":2}}}`
	s := aliMessagesFixture(t, ali.Provider{}, "qwen-plus", fixture, false)
	ev, err := s.Next()
	if err != nil || ev.Kind != gateway.EventData || string(ev.Payload) != fixture || ev.Usage == nil || ev.Usage.PromptTokens != 14 || ev.Usage.CompletionTokens != 4 || ev.Usage.CachedTokens != 5 || ev.Usage.CacheWriteTokens != 4 || ev.Usage.CacheWrite1hTokens != 2 {
		t.Fatalf("native body/tool/usage lost: %+v %v", ev, err)
	}
	if _, err := s.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("native body did not finish: %v", err)
	}
}

func TestLegacyMessagesHTTPChatFallback(t *testing.T) {
	fixture := `{"id":"chat1","choices":[{"message":{"role":"assistant","tool_calls":[{"id":"tool1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"hi\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":8,"completion_tokens":4,"prompt_tokens_details":{"cached_tokens":5}}}`
	s := aliMessagesFixture(t, ali.Provider{}, "deepseek-v3", fixture, false)
	ev, err := s.Next()
	if err != nil || ev.Kind != gateway.EventData || gjson.GetBytes(ev.Payload, "type").Str != "message" || gjson.GetBytes(ev.Payload, "content.0.type").Str != "tool_use" || gjson.GetBytes(ev.Payload, "content.0.input.q").Str != "hi" || ev.Usage == nil || ev.Usage.CachedTokens != 5 {
		t.Fatalf("actual fallback lost tools/usage: %s %+v %v", ev.Payload, ev.Usage, err)
	}
}

func TestLegacyMessagesHTTPStreamingChatFallback(t *testing.T) {
	fixture := "data: " + `{"id":"chat1","choices":[{"delta":{"tool_calls":[{"index":0,"id":"tool1","type":"function","function":{"name":"lookup","arguments":""}}]}}]}` + "\n\n" +
		"data: " + `{"id":"chat1","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":\"hi\"}"}}]}}]}` + "\n\n" +
		"data: " + `{"id":"chat1","choices":[{"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":8,"completion_tokens":4,"prompt_tokens_details":{"cached_tokens":5}}}` + "\n\n" +
		"data: [DONE]\n\n"
	s := aliMessagesFixture(t, ali.Provider{}, "deepseek-v3", fixture, true)
	var usage *gateway.Usage
	var args strings.Builder
	var start, stop bool
	for {
		ev, err := s.Next()
		if err != nil {
			t.Fatal(err)
		}
		if ev.Usage != nil {
			usage = ev.Usage
		}
		if ev.Kind == gateway.EventDone {
			break
		}
		if ev.Kind == gateway.EventError {
			t.Fatalf("fallback streaming error: %+v", ev.Err)
		}
		if ev.Kind != gateway.EventData {
			continue
		}
		if ev.Name == "message_start" {
			start = true
		}
		if ev.Name == "message_stop" {
			stop = true
		}
		if ev.Name == "content_block_delta" {
			args.WriteString(gjson.GetBytes(ev.Payload, "delta.partial_json").Str)
		}
		if string(ev.Payload) == "[DONE]" || ev.Name == "" {
			t.Fatalf("Chat protocol leaked into native Messages: %+v", ev)
		}
	}
	if !start || !stop || args.String() != `{"q":"hi"}` || usage == nil || usage.PromptTokens != 8 || usage.CompletionTokens != 4 || usage.CachedTokens != 5 {
		t.Fatalf("stream fallback lost tool/usage: start=%v stop=%v args=%s usage=%+v", start, stop, args.String(), usage)
	}
}

func aliSSE(events ...string) string {
	var fixture strings.Builder
	for _, event := range events {
		fixture.WriteString("event: ")
		fixture.WriteString(gjson.Get(event, "type").Str)
		fixture.WriteString("\ndata: ")
		fixture.WriteString(event)
		fixture.WriteString("\n\n")
	}
	return fixture.String()
}

func TestNativeMessagesHTTPStreamToolsAndTermination(t *testing.T) {
	events := []string{
		`{"type":"message_start","message":{"id":"m","usage":{"input_tokens":3,"cache_read_input_tokens":5}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"t","name":"lookup","input":{}}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"q\":\"hi\"}"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":4}}`,
		`{"type":"message_stop"}`,
	}
	s := aliMessagesFixture(t, ali.Provider{}, "qwen-plus", aliSSE(events...), true)
	for _, data := range events {
		ev, err := s.Next()
		name := gjson.Get(data, "type").Str
		if err != nil || ev.Kind != gateway.EventData || ev.Name != name || string(ev.Payload) != data {
			t.Fatalf("native SSE order changed: %+v %v", ev, err)
		}
		if name == "message_delta" && (ev.Usage == nil || ev.Usage.PromptTokens != 8 || ev.Usage.CompletionTokens != 4 || ev.Usage.CachedTokens != 5) {
			t.Fatalf("partial usage lost: %+v", ev.Usage)
		}
	}
	ev, err := s.Next()
	if err != nil || ev.Kind != gateway.EventDone {
		t.Fatalf("native marker missing: %+v %v", ev, err)
	}
}

func TestNativeMessagesHTTPErrorAndEmpty(t *testing.T) {
	for _, tc := range []struct {
		name, fixture string
		stream        bool
		kind          gateway.EventKind
		wantEOF       bool
	}{
		{"body error", `{"type":"error","error":{"type":"invalid_request_error","message":"bad"}}`, false, gateway.EventError, false},
		{"empty body", `{"content":[],"usage":{"input_tokens":0}}`, false, 0, true},
		{"hidden stream error", aliSSE(`{"type":"message_start","message":{"id":"m"}}`, `{"type":"error","error":{"type":"overloaded_error","message":"busy"}}`), true, gateway.EventError, false},
		{"empty lifecycle", aliSSE(`{"type":"message_start","message":{"id":"m","usage":{"input_tokens":0}}}`, `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":0}}`, `{"type":"message_stop"}`), true, gateway.EventUsage, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := aliMessagesFixture(t, ali.Provider{}, "qwen-plus", tc.fixture, tc.stream)
			ev, err := s.Next()
			if tc.wantEOF {
				if !errors.Is(err, io.EOF) {
					t.Fatalf("empty body became visible: %+v %v", ev, err)
				}
				return
			}
			if err != nil || ev.Kind != tc.kind {
				t.Fatalf("wrong failure/empty event: %+v %v", ev, err)
			}
		})
	}
}

func TestNativeMessagesUnsupportedFallbackFieldsAreExplicit(t *testing.T) {
	body, _ := json.Marshal(struct {
		Model    string `json:"model"`
		Messages []any  `json:"messages"`
		Thinking any    `json:"thinking"`
	}{"deepseek-v3", []any{}, struct {
		Type string `json:"type"`
	}{"enabled"}})
	_, err := (ali.Provider{}).BuildRequest(context.Background(), &gateway.Request{Protocol: gateway.ProtocolAnthropic, Model: "deepseek-v3", Body: body}, gateway.Target{BaseURL: "https://example.test"})
	var upstream *gateway.UpstreamError
	if !errors.As(err, &upstream) || upstream.Status != 400 || upstream.Code != "unsupported_protocol_conversion" {
		t.Fatalf("unsupported fallback silently dropped fields: %v", err)
	}
}
