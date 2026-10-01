package ali_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/ali"
	"github.com/tidwall/gjson"
)

func TestPluginFromChannelSettingsAndCompatiblePath(t *testing.T) {
	body := []byte(`{"model":"client","messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`)
	for _, base := range []string{"https://dashscope.example", "https://dashscope.example/compatible-mode", "https://dashscope.example/compatible-mode/v1/", "https://dashscope.example/compatible-mode/v1/chat/completions"} {
		out, err := (ali.Provider{}).BuildRequest(context.Background(), &gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Body: body, Model: "client", Stream: true}, gateway.Target{BaseURL: base, Secret: "test-key", UpstreamModel: "qwen-plus", Settings: map[string]any{"plugin": "plugin-name"}})
		if err != nil {
			t.Fatal(err)
		}
		if out.URL.String() != "https://dashscope.example/compatible-mode/v1/chat/completions" || out.Header.Get("X-DashScope-Plugin") != "plugin-name" || out.Header.Get("X-DashScope-SSE") != "enable" || out.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("request=%s %v", out.URL, out.Header)
		}
		data, err := io.ReadAll(out.Body)
		if err != nil {
			t.Fatal(err)
		}
		if gjson.GetBytes(data, "model").Str != "qwen-plus" || gjson.GetBytes(data, "tools.0.function.name").Str != "lookup" || !gjson.GetBytes(data, "stream_options.include_usage").Bool() || gjson.GetBytes(body, "model").Str != "client" {
			t.Fatalf("body=%s; original=%s", data, body)
		}
	}
}

func TestNativeResponsesKeepsSpecialEndpointAndPlugin(t *testing.T) {
	out, err := (ali.Provider{}).BuildRequest(context.Background(), &gateway.Request{Protocol: gateway.ProtocolResponses, Body: []byte(`{"model":"client","input":"hello","previous_response_id":"r1"}`), Model: "client"}, gateway.Target{BaseURL: "https://dashscope.example/proxy?feature=one", Secret: "test-key", Settings: map[string]any{"plugin": "plugin-name"}})
	if err != nil {
		t.Fatal(err)
	}
	if out.URL.Path != "/proxy/api/v2/apps/protocols/compatible-mode/v1/responses" || out.URL.Query().Get("feature") != "one" || out.Header.Get("X-DashScope-Plugin") != "plugin-name" || out.Header.Get("X-DashScope-SSE") != "" {
		t.Fatalf("request=%s %v", out.URL, out.Header)
	}
	data, err := io.ReadAll(out.Body)
	if err != nil {
		t.Fatal(err)
	}
	if gjson.GetBytes(data, "previous_response_id").Str != "r1" || gjson.GetBytes(data, "stream_options").Exists() {
		t.Fatalf("Responses fields changed: %s", data)
	}
}

func TestInvalidPluginDoesNotSilentlyDisappear(t *testing.T) {
	for _, value := range []any{true, 42, []any{"plugin"}, map[string]any{"id": "plugin"}, "plugin\r\nAuthorization: injected"} {
		_, err := (ali.Provider{}).BuildRequest(context.Background(), &gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Body: []byte(`{}`)}, gateway.Target{BaseURL: "https://dashscope.example", Secret: "must-not-leak", Settings: map[string]any{"plugin": value}})
		var upstream *gateway.UpstreamError
		if !errors.As(err, &upstream) || upstream.Status != http.StatusBadGateway || upstream.Code != "invalid_channel_plugin" || strings.Contains(err.Error(), "must-not-leak") {
			t.Fatalf("value %T error=%v", value, err)
		}
	}
	for _, settings := range []map[string]any{nil, {"plugin": nil}, {"plugin": ""}} {
		out, err := (ali.Provider{}).BuildRequest(context.Background(), &gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Body: []byte(`{}`)}, gateway.Target{Settings: settings})
		if err != nil || out.Header.Get("X-DashScope-Plugin") != "" {
			t.Fatalf("empty plugin=%v %v", out, err)
		}
	}
}

func TestDecodeUsesCanonicalUsageAndErrorHandling(t *testing.T) {
	for _, tc := range []struct {
		protocol gateway.Protocol
		body     string
		kind     gateway.EventKind
		tokens   int64
	}{
		{gateway.ProtocolOpenAIChat, `{"choices":[{"message":{"content":"hello"}}],"usage":{"prompt_tokens":4,"completion_tokens":2}}`, gateway.EventData, 2},
		{gateway.ProtocolResponses, `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":4,"output_tokens":2}}`, gateway.EventData, 2},
		{gateway.ProtocolOpenAIChat, `{"error":{"code":"bad_key","message":"failed"}}`, gateway.EventError, 0},
	} {
		s := (ali.Provider{}).Decode(&gateway.Request{Protocol: tc.protocol}, &http.Response{Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.body))})
		ev, err := s.Next()
		_ = s.Close()
		if err != nil || ev.Kind != tc.kind || (tc.tokens > 0 && (ev.Usage == nil || ev.Usage.CompletionTokens != tc.tokens)) {
			t.Fatalf("event=%+v %v", ev, err)
		}
	}
}
