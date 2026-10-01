package gateway_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/anthropic"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/codex"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/gemini"
)

func fixtureGateway(t *testing.T, upstream string, target gateway.Target, provider gateway.Provider) *harness {
	t.Helper()
	target.BaseURL = upstream
	planner := &fakePlanner{targets: []gateway.Target{target, target}}
	settler := newSettler()
	g, err := gateway.New(gateway.Deps{Authorizer: fakeAuth{}, Planner: planner, Settler: settler, Providers: map[string]gateway.Provider{target.Provider: provider}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	g.Register(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return &harness{t: t, gw: server, planner: planner, settler: settler}
}

func TestProtocolHeadersUseExplicitAllowlist(t *testing.T) {
	for _, tc := range []struct {
		name, path, body, reply string
		target                  gateway.Target
		provider                gateway.Provider
	}{
		{"anthropic", "/v1/messages", `{"model":"claude","max_tokens":10,"messages":[{"role":"user","content":"hello"}]}`, `{"id":"msg1","content":[{"type":"text","text":"hello"}],"usage":{"input_tokens":3,"output_tokens":2}}`, gateway.Target{Provider: "anthropic", Secret: "upstream-key"}, anthropic.Provider{}},
		{"codex", "/v1/responses", `{"model":"gpt","input":"hello"}`, `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":3,"output_tokens":2}}`, gateway.Target{Provider: "codex", Secret: `{"access_token":"upstream-key","account_id":"upstream-account"}`}, codex.Provider{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			captured := make(chan http.Header, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured <- r.Header.Clone()
				w.Header().Set("X-Codex-Turn-State", "server-state")
				w.Header().Set("Chatgpt-Account-Id", "private-upstream-account")
				_, _ = w.Write([]byte(tc.reply))
			}))
			t.Cleanup(upstream.Close)
			h := fixtureGateway(t, upstream.URL, tc.target, tc.provider)
			r, _ := http.NewRequest(http.MethodPost, h.gw.URL+tc.path, strings.NewReader(tc.body))
			r.Header.Set("Authorization", "Bearer sk-test")
			r.Header.Set("Anthropic-Beta", "context-management-2025-06-27")
			r.Header.Set("X-Codex-Turn-State", "client-state")
			r.Header.Set("Chatgpt-Account-Id", "untrusted-account")
			resp, err := http.DefaultClient.Do(r)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			_ = h.outcome()
			headers := <-captured
			if headers.Get("Authorization") == "Bearer sk-test" || headers.Get("Chatgpt-Account-Id") == "untrusted-account" || resp.Header.Get("Chatgpt-Account-Id") != "" {
				t.Fatalf("private headers crossed trust boundary")
			}
			if tc.name == "anthropic" && headers.Get("Anthropic-Beta") != "context-management-2025-06-27" {
				t.Fatal("dropped Anthropic beta")
			}
			if tc.name == "codex" && (headers.Get("X-Codex-Turn-State") != "client-state" || resp.Header.Get("X-Codex-Turn-State") != "server-state") {
				t.Fatal("dropped Codex turn state")
			}
		})
	}
}

func TestConversion400DoesNotCallUpstreamRetryOrCoolCredentials(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	t.Cleanup(upstream.Close)
	h := fixtureGateway(t, upstream.URL, gateway.Target{Provider: "gemini"}, gemini.Provider{})
	view := h.do(`{"model":"gpt","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"http://127.0.0.1/image.png"}}]}]}`)
	out := h.outcome()
	results := h.planner.results()
	if view.status != 400 || calls.Load() != 0 || len(results) != 1 || results[0].Retryable || results[0].Scope != gateway.ScopeRequest || out.Charge {
		t.Fatalf("view=%+v calls=%d results=%+v outcome=%+v", view, calls.Load(), results, out)
	}
}
