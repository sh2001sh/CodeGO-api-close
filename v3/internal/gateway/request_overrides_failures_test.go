package gateway_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/openai"
)

func TestOverridePipelineRejectsInvalidConfigurationWithoutDispatch(t *testing.T) {
	for _, test := range []struct {
		name   string
		target gateway.Target
	}{
		{"malformed operations", gateway.Target{ParamOverride: map[string]any{"operations": "invalid"}}},
		{"invalid force format", gateway.Target{Settings: map[string]any{"force_format": "true"}}},
		{"invalid thinking conversion", gateway.Target{Settings: map[string]any{"thinking_to_content": 1}}},
		{"invalid system prompt", gateway.Target{Settings: map[string]any{"system_prompt": 1}}},
		{"invalid status mapping", gateway.Target{StatusCodeMapping: map[string]int{"429": 700}}},
		{"invalid header override", gateway.Target{HeaderOverride: map[string]string{"X-Policy": "bad\r\nvalue"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				_, _ = w.Write([]byte(`{"choices":[{"index":0,"message":{"content":"must not run"}}]}`))
			}))
			t.Cleanup(upstream.Close)
			test.target.Provider = openai.ID
			h := fixtureGateway(t, upstream.URL, test.target, openai.Provider{})
			view, out := h.do(streamBody), h.outcome()
			reports := h.planner.results()
			if view.status != 400 || calls.Load() != 0 || len(reports) != 1 || reports[0].Retryable || reports[0].Scope != gateway.ScopeRequest || out.Charge || out.Terminal != gateway.TerminalUpstreamErrorBeforeOutput {
				t.Fatalf("invalid config dispatched/retried/charged: view=%+v calls=%d reports=%+v out=%+v", view, calls.Load(), reports, out)
			}
			h.settler.mu.Lock()
			reserved := h.settler.reserved
			h.settler.mu.Unlock()
			if reserved != 1 {
				t.Fatalf("expected exactly one released reservation, got %d", reserved)
			}
		})
	}
}

func TestOverridePipelineIntentionalErrorRetryContract(t *testing.T) {
	for _, skip := range []bool{true, false} {
		t.Run(map[bool]string{true: "skip", false: "retry"}[skip], func(t *testing.T) {
			var calls atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				_, _ = w.Write([]byte(`{"choices":[{"index":0,"message":{"content":"fallback"}}],"usage":{"prompt_tokens":1,"completion_tokens":2}}`))
			}))
			t.Cleanup(upstream.Close)
			target := gateway.Target{Provider: openai.ID, BaseURL: upstream.URL, ParamOverride: map[string]any{"operations": []any{map[string]any{
				"mode": "return_error", "value": map[string]any{"message": "policy blocked", "status_code": 500, "code": "policy", "skip_retry": skip},
			}}}}
			h := clientIdentityHarness(t, target, openai.Provider{}, nil)
			h.planner.targets = append(h.planner.targets, gateway.Target{Provider: openai.ID, BaseURL: upstream.URL})
			view, out := h.do(`{"model":"gpt-test","messages":[{"role":"user","content":"hi"}]}`), h.outcome()
			reports := h.planner.results()
			if skip {
				if view.status != 500 || calls.Load() != 0 || len(reports) != 1 || reports[0].Retryable || reports[0].Scope != gateway.ScopeRequest || out.Charge || out.Err == nil || out.Err.Code != "policy" {
					t.Fatalf("skip_retry failed: view=%+v calls=%d reports=%+v out=%+v", view, calls.Load(), reports, out)
				}
			} else if view.status != 200 || calls.Load() != 1 || len(reports) != 2 || !reports[0].Retryable || !out.Charge || out.Usage.CompletionTokens != 2 {
				t.Fatalf("explicit retry did not reach fallback: view=%+v calls=%d reports=%+v out=%+v", view, calls.Load(), reports, out)
			}
		})
	}
}

func TestOverridePipelineMappedStatusControlsExecution(t *testing.T) {
	for _, upstreamStatus := range []int{429, 500} {
		t.Run(map[int]string{429: "reject_mapped_400", 500: "decode_mapped_200"}[upstreamStatus], func(t *testing.T) {
			var calls atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(upstreamStatus)
				if upstreamStatus == 429 {
					_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
				} else {
					_, _ = w.Write([]byte(`{"choices":[{"index":0,"message":{"content":"mapped success"}}],"usage":{"prompt_tokens":1,"completion_tokens":2}}`))
				}
			}))
			t.Cleanup(upstream.Close)
			target := gateway.Target{Provider: openai.ID, StatusCodeMapping: map[string]int{"429": 400, "500": 200}}
			h := fixtureGateway(t, upstream.URL, target, openai.Provider{})
			view, out := h.do(`{"model":"gpt-test","messages":[{"role":"user","content":"hi"}]}`), h.outcome()
			reports := h.planner.results()
			if calls.Load() != 1 || len(reports) != 1 || reports[0].Retryable {
				t.Fatalf("mapped status retried: calls=%d reports=%+v", calls.Load(), reports)
			}
			if upstreamStatus == 429 {
				if view.status != 400 || out.Charge || reports[0].Scope != gateway.ScopeRequest || reports[0].Status != 400 {
					t.Fatalf("mapped reject misclassified: view=%+v out=%+v reports=%+v", view, out, reports)
				}
			} else if view.status != 200 || !out.Charge || out.Terminal != gateway.TerminalCompleted || out.Usage.CompletionTokens != 2 || !reports[0].OK {
				t.Fatalf("mapped success did not decode: view=%+v out=%+v reports=%+v", view, out, reports)
			}
		})
	}
}
