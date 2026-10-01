package auxiliary

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type policyAdapterSpy struct {
	Adapter
	builds atomic.Int64
}

func (a *policyAdapterSpy) Build(ctx context.Context, req *gateway.Request, target gateway.Target, input Input) (*http.Request, error) {
	a.builds.Add(1)
	return a.Adapter.Build(ctx, req, target, input)
}

func TestAuxiliaryTargetPolicyRejectsBeforeReserveOrAdapterBuild(t *testing.T) {
	h, plan, settle, limits := testHandler(t, "http://unused.invalid")
	spy := &policyAdapterSpy{Adapter: h.adapters["openai"]}
	h.adapters["openai"] = spy
	const path, body = "/v1/embeddings", `{"model":"alias","input":"original"}`
	h.cfg.TargetPolicy = func(req *gateway.Request, target gateway.Target) error {
		if req.Path != path || string(req.Body) != body || req.PricingHeaders["X-Codego-Operation"] != string(Embeddings) || req.Model != "alias" || target.ChannelID != 1 {
			t.Error("frozen policy context lost")
		}
		return &gateway.UpstreamError{Status: 403, Type: "permission_error", Code: "policy_denied", Message: "request denied"}
	}
	w := invoke(h, path, body)
	if w.Code != 403 || settle.reserves != 0 || settle.finalized || spy.builds.Load() != 0 || limits.acquired != 0 || len(plan.reports) != 0 {
		t.Fatalf("status=%d reserves=%d builds=%d reports=%v", w.Code, settle.reserves, spy.builds.Load(), plan.reports)
	}
	h.cfg.TargetPolicy = func(*gateway.Request, gateway.Target) error { return errors.New("private policy detail") }
	w = invoke(h, path, body)
	if w.Code != 503 || strings.Contains(w.Body.String(), "private policy detail") || settle.reserves != 0 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAuxiliaryRetryTargetPolicyRefundsWithoutSubmissionOrCooling(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(503) }))
	defer server.Close()
	h, plan, settle, limits := testHandler(t, server.URL, server.URL)
	spy := &policyAdapterSpy{Adapter: h.adapters["openai"]}
	h.adapters["openai"] = spy
	var policyCalls int
	h.cfg.TargetPolicy = func(req *gateway.Request, target gateway.Target) error {
		policyCalls++
		if req.Path != "/v1/embeddings" || string(req.Body) != `{"model":"alias","input":"original"}` {
			t.Error("retry did not preserve prompt")
		}
		if target.ChannelID == 2 {
			return &gateway.UpstreamError{Status: 403, Type: "permission_error", Code: "policy_denied", Message: "request denied"}
		}
		return nil
	}
	w := invoke(h, "/v1/embeddings", `{"model":"alias","input":"original"}`)
	if w.Code != 403 || calls.Load() != 1 || spy.builds.Load() != 1 || settle.reserves != 1 || !settle.finalized || settle.out.Charge || len(plan.reports) != 1 || policyCalls != 2 || limits.acquired != 1 || limits.released != 1 {
		t.Fatalf("status=%d calls=%d builds=%d policycalls=%d outcome=%+v reports=%v limits=%+v", w.Code, calls.Load(), spy.builds.Load(), policyCalls, settle.out, plan.reports, limits)
	}
	attempt := settle.req.Attempts[len(settle.req.Attempts)-1]
	if len(settle.req.Attempts) != 2 || attempt.Result.Scope != gateway.ScopeRequest || attempt.Result.Retryable || attempt.Result.Status != 403 {
		t.Fatalf("attempts=%+v", settle.req.Attempts)
	}
}

func TestAuxiliarySensitiveFactoryCoversActualOperationPrompts(t *testing.T) {
	for _, test := range []struct{ name, provider, path, body, response string }{
		{"embeddings", "openai", "/v1/embeddings", `{"model":"alias","input":["blocked text"]}`, `{"data":[{"embedding":[1]}],"usage":{"prompt_tokens":1}}`},
		{"speech", "openai", "/v1/audio/speech", `{"model":"alias","input":"blocked text","voice":"alloy","response_format":"pcm"}`, "binary audio"},
		{"images", "openai", "/v1/images/generations", `{"model":"alias","prompt":"blocked text"}`, `{"data":[{"b64_json":"aW1hZ2U="}]}`},
		{"rerank query", "openai", "/v1/rerank", `{"model":"alias","query":"blocked text","documents":["safe document"]}`, `{"results":[{"index":0,"relevance_score":1}]}`},
		{"rerank document", "openai", "/v1/rerank", `{"model":"alias","query":"safe query","documents":["blocked text"]}`, `{"results":[{"index":0,"relevance_score":1}]}`},
		{"legacy completions", "openai", "/v1/completions", `{"model":"alias","prompt":"blocked text"}`, `{"choices":[{"text":"result"}]}`},
		{"Gemini embed", "gemini", "/v1beta/models/alias:embedContent", `{"content":{"parts":[{"text":"blocked text"}]}}`, `{"embedding":{"values":[1]}}`},
		{"Gemini batch", "gemini", "/v1beta/models/alias:batchEmbedContents", `{"requests":[{"content":{"parts":[{"text":"blocked text"}]}}]}`, `{"embeddings":[{"values":[1]}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); _, _ = io.WriteString(w, test.response) }))
			defer server.Close()
			h, plan, settle, limits := testHandler(t, server.URL)
			plan.targets[0].Provider = test.provider
			spy := &policyAdapterSpy{Adapter: h.adapters[test.provider]}
			h.adapters[test.provider] = spy
			settings := map[string]json.RawMessage{"SensitiveWords": json.RawMessage(`["contains:blocked"]`)}
			h.cfg.TargetPolicy = gateway.NewSensitiveWordPolicy(func() map[string]json.RawMessage { return settings }, slog.New(slog.NewTextHandler(io.Discard, nil)))
			w := invoke(h, test.path, test.body)
			if w.Code != 403 || settle.reserves != 0 || spy.builds.Load() != 0 || calls.Load() != 0 || limits.acquired != 0 {
				t.Fatalf("sensitive prompt bypass: status=%d reserves=%d builds=%d calls=%d body=%s", w.Code, settle.reserves, spy.builds.Load(), calls.Load(), w.Body.String())
			}
			plan.targets[0].Settings = map[string]any{"sensitive_word_interception_enabled": false}
			w = invoke(h, test.path, test.body)
			if w.Code != 200 || settle.reserves != 1 || !settle.out.Charge || spy.builds.Load() != 1 || calls.Load() != 1 {
				t.Fatalf("channel optout status=%d outcome=%+v body=%s", w.Code, settle.out, w.Body.String())
			}
			plan.targets[0].Settings["sensitive_word_interception_enabled"] = true
			safe := strings.TrimSuffix(strings.ReplaceAll(test.body, "blocked text", "safe text"), "}") + `,"metadata":{"note":"blocked text"}}`
			w = invoke(h, test.path, safe)
			if w.Code != 200 || settle.reserves != 2 || spy.builds.Load() != 2 || calls.Load() != 2 {
				t.Fatalf("nonprompt metadata caused false denial status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}
