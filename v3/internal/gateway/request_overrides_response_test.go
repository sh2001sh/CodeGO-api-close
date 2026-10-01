package gateway_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/bridge"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/gemini"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/openai"
	"github.com/tidwall/gjson"
)

func TestOverridePipelineResponsePresentationPreservesBilling(t *testing.T) {
	for _, actualUsage := range []bool{false, true} {
		t.Run(map[bool]string{false: "estimated", true: "actual"}[actualUsage], func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: {\"id\":\"chat\",\"vendor\":true,\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"abc\",\"vendor\":true}}]}\n\n"))
				_, _ = w.Write([]byte("data: {\"id\":\"chat\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning\":\"de\",\"content\":\"OK\"},\"finish_reason\":\"stop\"}]}\n\n"))
				if actualUsage {
					_, _ = w.Write([]byte("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":17,\"completion_tokens\":37}}\n\n"))
				}
				_, _ = w.Write([]byte("data: [DONE]\n\n"))
			}))
			t.Cleanup(upstream.Close)
			target := gateway.Target{Provider: openai.ID, Settings: map[string]any{"force_format": true, "thinking_to_content": true}}
			h := fixtureGateway(t, upstream.URL, target, openai.Provider{})
			view, out := h.do(streamBody), h.outcome()
			if view.status != 200 || len(view.data) != 3 || view.data[2] != "[DONE]" || !out.Charge {
				t.Fatalf("response pipeline failed: view=%+v out=%+v", view, out)
			}
			if gjson.Get(view.data[0], "choices.0.delta.content").String() != "<think>\nabc" || gjson.Get(view.data[1], "choices.0.delta.content").String() != "de\n</think>\nOK" {
				t.Fatalf("reasoning or simultaneous content lost: %v", view.data)
			}
			if gjson.Get(view.data[0], "vendor").Exists() || gjson.Get(view.data[0], "choices.0.delta.vendor").Exists() || gjson.Get(view.data[1], "choices.0.delta.reasoning").Exists() {
				t.Fatal("response settings did not project/remove converted fields")
			}
			if actualUsage {
				if out.Terminal != gateway.TerminalCompleted || out.Usage.Estimated || out.Usage.CompletionTokens != 37 || out.Usage.PromptTokens != 17 {
					t.Fatalf("presentation changed upstream usage: %+v", out)
				}
			} else if out.Terminal != gateway.TerminalCompletedNoUsage || !out.Usage.Estimated || out.Usage.CompletionTokens != 2 || out.Usage.PromptTokens != int64(len(streamBody)+3)/4 {
				t.Fatalf("think markup entered usage estimate: %+v", out)
			}
		})
	}
}

func TestOverridePipelineNonstreamFormatKeepsReasoning(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chat","vendor":true,"choices":[{"index":0,"message":{"role":"assistant","content":"OK","reasoning_content":"why","vendor":true},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2}}`))
	}))
	t.Cleanup(upstream.Close)
	target := gateway.Target{Provider: openai.ID, Settings: map[string]any{"force_format": true, "thinking_to_content": true}}
	h := fixtureGateway(t, upstream.URL, target, openai.Provider{})
	view, out := h.do(`{"model":"gpt-test","messages":[{"role":"user","content":"hi"}]}`), h.outcome()
	if view.status != 200 || !out.Charge || out.Usage.CompletionTokens != 2 || gjson.Get(view.body, "vendor").Exists() || gjson.Get(view.body, "choices.0.message.vendor").Exists() || gjson.Get(view.body, "choices.0.message.reasoning_content").String() != "why" || strings.Contains(view.body, "<think>") {
		t.Fatalf("nonstream presentation or accounting changed: view=%+v out=%+v", view, out)
	}
}

func TestOverridePipelineRetryConditionsSeePreviousFailure(t *testing.T) {
	seen := make(chan []byte, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		seen <- body
		if !gjson.GetBytes(body, "retry_policy").Exists() {
			w.WriteHeader(500)
			_, _ = w.Write([]byte(`{"error":{"message":"first failed"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"index":0,"message":{"content":"retry completed"}}],"usage":{"prompt_tokens":1,"completion_tokens":2}}`))
	}))
	t.Cleanup(upstream.Close)
	target := gateway.Target{Provider: openai.ID, ParamOverride: map[string]any{"operations": []any{map[string]any{
		"mode": "set", "path": "retry_policy", "value": "fallback", "logic": "AND",
		// last_error carries the client-safe 502 error for an HTTP 500.
		"conditions": map[string]any{"is_retry": true, "retry_index": 1, "last_error_status_code": 502},
	}}}}
	h := fixtureGateway(t, upstream.URL, target, openai.Provider{})
	view, out := h.do(`{"model":"gpt-test","messages":[{"role":"user","content":"hi"}]}`), h.outcome()
	if view.status != 200 || !out.Charge || len(h.planner.results()) != 2 {
		t.Fatalf("retry context was not refreshed: view=%+v reports=%+v out=%+v", view, h.planner.results(), out)
	}
	if gjson.GetBytes(<-seen, "retry_policy").Exists() || gjson.GetBytes(<-seen, "retry_policy").String() != "fallback" {
		t.Fatal("conditional operation did not follow actual previous failure")
	}
}

func TestOverridePipelineRawForwardingBypassesNativeConversion(t *testing.T) {
	seen := make(chan []byte, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		seen <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"raw forwarded"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2}}`))
	}))
	t.Cleanup(upstream.Close)
	target := gateway.Target{Provider: "raw-gemini", UpstreamModel: "mapped-model", Settings: map[string]any{"pass_through_body_enabled": true, "system_prompt": "must-not-inject"}, ParamOverride: map[string]any{"temperature": 0.2}}
	h := fixtureGateway(t, upstream.URL, target, bridge.Provider{Chat: gemini.Provider{}})
	// Conversion would reject/fetch this private URL; raw forwarding must only
	// send the administrator-selected original bytes to the configured endpoint.
	body := `{"model":"alias","store":true,"service_tier":"priority","vendor_keep":true,"tools":[{"type":"function","function":{"name":"raw_tool","parameters":{"vendor_type":"raw"}}}],"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"http://127.0.0.1/private.png"}}]}]}`
	view, out := h.do(body), h.outcome()
	if view.status != 200 || !out.Charge || out.Usage.CompletionTokens != 2 || !strings.Contains(view.body, "raw forwarded") {
		t.Fatalf("raw forwarding still performed native conversion: view=%+v out=%+v", view, out)
	}
	actual := <-seen
	if gjson.GetBytes(actual, "contents").Exists() || gjson.GetBytes(actual, "model").String() != "alias" || !gjson.GetBytes(actual, "store").Bool() || !gjson.GetBytes(actual, "vendor_keep").Bool() || gjson.GetBytes(actual, "service_tier").String() != "priority" || gjson.GetBytes(actual, "temperature").Float() != 0.2 || gjson.GetBytes(actual, "messages.0.content.0.image_url.url").String() != "http://127.0.0.1/private.png" {
		t.Fatalf("raw payload was converted/filtered or override lost: %s", actual)
	}
	if gjson.GetBytes(actual, "tools.0.function.name").String() != "raw_tool" || gjson.GetBytes(actual, "tools.0.function.parameters.vendor_type").String() != "raw" || gjson.GetBytes(actual, "systemInstruction").Exists() || gjson.GetBytes(actual, "stream_options").Exists() || strings.Contains(string(actual), "must-not-inject") {
		t.Fatalf("raw tools changed or system/usage fields were injected: %s", actual)
	}
}
