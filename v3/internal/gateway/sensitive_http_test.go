package gateway_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/bridge"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/gemini"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/openai"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/responses"
)

type sensitiveHTTPHarness struct {
	server  *httptest.Server
	planner *fakePlanner
	settler *fakeSettler
	calls   *atomic.Int64
}

func newSensitiveHTTPHarness(t *testing.T, settings func() map[string]json.RawMessage, logger *slog.Logger, targets ...gateway.Target) *sensitiveHTTPHarness {
	t.Helper()
	calls := &atomic.Int64{}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("X-Channel") == "first-fails" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		reply := `{"choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`
		if strings.HasSuffix(r.URL.Path, "/responses") {
			reply = `{"id":"r1","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":3,"output_tokens":2}}`
		}
		if strings.Contains(r.URL.Path, ":generateContent") {
			reply = `{"candidates":[{"content":{"parts":[{"text":"hello"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2,"totalTokenCount":5}}`
		}
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(upstream.Close)
	if len(targets) == 0 {
		targets = []gateway.Target{{ChannelID: 1}}
	}
	for i := range targets {
		targets[i].Provider = "test"
		targets[i].BaseURL = upstream.URL
		targets[i].Secret = "test-upstream-key"
	}
	planner := &fakePlanner{targets: targets}
	settler := newSettler()
	settler.outcomes = make(chan gateway.Outcome, 128)
	provider := bridge.Provider{Chat: openai.Provider{}, Native: map[gateway.Protocol]gateway.Provider{gateway.ProtocolResponses: responses.Provider{}, gateway.ProtocolGemini: gemini.Provider{}}}
	g, err := gateway.New(gateway.Deps{Authorizer: fakeAuth{}, Planner: planner, Settler: settler, Providers: map[string]gateway.Provider{"test": provider}, TargetPolicy: gateway.NewSensitiveWordPolicy(settings, logger)})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	g.Register(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return &sensitiveHTTPHarness{server: server, planner: planner, settler: settler, calls: calls}
}

func (h *sensitiveHTTPHarness) reserves() int {
	h.settler.mu.Lock()
	defer h.settler.mu.Unlock()
	return h.settler.reserved
}

func (h *sensitiveHTTPHarness) request(t *testing.T, path, body string) (int, string, *gateway.Outcome) {
	t.Helper()
	before := h.reserves()
	r, err := http.NewRequestWithContext(context.Background(), http.MethodPost, h.server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer sk-test")
	r.Header.Set("Content-Type", "application/json")
	resp, err := h.server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err = resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if h.reserves() > before {
		select {
		case outcome := <-h.settler.outcomes:
			return resp.StatusCode, string(payload), &outcome
		case <-time.After(2 * time.Second):
			t.Fatal("reserved request was not finalized")
		}
	}
	return resp.StatusCode, string(payload), nil
}

func sensitiveChatBody(text string) string {
	encoded, _ := json.Marshal(map[string]any{"model": "test-model", "messages": []any{map[string]any{"role": "user", "content": text}}})
	return string(encoded)
}

func sensitiveValues(words any, flags map[string]any) func() map[string]json.RawMessage {
	values := map[string]json.RawMessage{}
	for key, value := range flags {
		values[key], _ = json.Marshal(value)
	}
	if words != nil {
		values["SensitiveWords"], _ = json.Marshal(words)
	}
	return func() map[string]json.RawMessage { return values }
}

func TestSensitiveDefaultRulesRejectBeforeReserveOrUpstream(t *testing.T) {
	h := newSensitiveHTTPHarness(t, nil, nil)
	for _, prompt := range []string{"credential stuffing", "steal browser cookies", "sql injection payload", "bypass rate limit", "bypass moderation", "evade safety filter", "reverse shell", "privilege escalation", "keylogger", "ransomware", "malware loader", "crack password", "phishing page"} {
		status, body, out := h.request(t, "/v1/chat/completions", sensitiveChatBody(strings.ToUpper(prompt)))
		if status != 403 || !strings.Contains(body, "sensitive_words_detected") || out != nil {
			t.Fatalf("default rule failed for %q: %d %s", prompt, status, body)
		}
	}
	if h.calls.Load() != 0 || h.reserves() != 0 || len(h.planner.results()) != 0 {
		t.Fatal("blocked first target reached upstream, reserve or cooldown")
	}
}

func TestSensitiveChannelDefaultOptOutAndAnthropicCallerExemption(t *testing.T) {
	for _, test := range []struct {
		name, path, body string
		settings         map[string]any
		want             int
	}{
		{"default", "/v1/chat/completions", sensitiveChatBody("reverse shell"), nil, 403},
		{"nil default", "/v1/chat/completions", sensitiveChatBody("reverse shell"), map[string]any{"sensitive_word_interception_enabled": nil}, 403},
		{"optout", "/v1/chat/completions", sensitiveChatBody("reverse shell"), map[string]any{"sensitive_word_interception_enabled": false}, 200},
		{"invalid channel flag", "/v1/chat/completions", sensitiveChatBody("hello"), map[string]any{"sensitive_word_interception_enabled": "true"}, 503},
		{"anthropic caller with openai upstream", "/v1/messages", `{"model":"test-model","max_tokens":10,"messages":[{"role":"user","content":"reverse shell"}]}`, nil, 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newSensitiveHTTPHarness(t, nil, nil, gateway.Target{ChannelID: 9, Settings: test.settings})
			status, body, out := h.request(t, test.path, test.body)
			if status != test.want {
				t.Fatalf("got %d want %d %s", status, test.want, body)
			}
			if test.want != 200 && (h.calls.Load() != 0 || h.reserves() != 0 || out != nil) {
				t.Fatal("policy rejection reserved or forwarded request")
			}
			if test.want == 200 && (h.calls.Load() != 1 || h.reserves() != 1 || out == nil) {
				t.Fatal("optout/exempt request did not run real pipeline")
			}
		})
	}
}

func TestSensitiveLaterCandidateBlocksAndRefundsWithoutExecution(t *testing.T) {
	h := newSensitiveHTTPHarness(t, nil, nil,
		gateway.Target{ChannelID: 1, Settings: map[string]any{"sensitive_word_interception_enabled": false}, HeaderOverride: map[string]string{"X-Channel": "first-fails"}},
		gateway.Target{ChannelID: 2})
	status, body, out := h.request(t, "/v1/chat/completions", sensitiveChatBody("reverse shell"))
	reports := h.planner.results()
	if status != 403 || !strings.Contains(body, "sensitive_words_detected") || h.calls.Load() != 1 || h.reserves() != 1 || out == nil || out.Charge || out.Usage.PromptTokens != 0 {
		t.Fatalf("later policy did not refund/stop: %d %s %+v", status, body, out)
	}
	if len(reports) != 2 || !reports[0].Retryable || reports[1].Retryable || reports[1].Scope != gateway.ScopeRequest || reports[1].Status != 403 {
		t.Fatalf("policy became channel failure: %+v", reports)
	}
}
