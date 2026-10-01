package gateway_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/anthropic"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/bridge"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/openai"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/responses"
	"github.com/tidwall/gjson"
)

type pipelineFinalizerProvider struct {
	gateway.Provider
	originalBody    string
	originalHeaders map[string]string
	finalized       chan string
}

func (p *pipelineFinalizerProvider) BuildRequest(ctx context.Context, req *gateway.Request, target gateway.Target) (*http.Request, error) {
	if p.originalBody == "" {
		p.originalBody = string(req.Body)
	}
	p.originalHeaders = maps.Clone(req.PricingHeaders)
	return p.Provider.BuildRequest(ctx, req, target)
}

func (p *pipelineFinalizerProvider) FinalizeRequest(_ context.Context, out *http.Request, req *gateway.Request, _ gateway.Target) error {
	if string(req.Body) != p.originalBody || !reflect.DeepEqual(req.PricingHeaders, p.originalHeaders) {
		return errors.New("override mutated original pricing inputs")
	}
	if req.PricingHeaders["Authorization"] != "" {
		return errors.New("client authentication entered pricing inputs")
	}
	copyBody, err := out.GetBody()
	if err != nil {
		return err
	}
	finalBytes, err := io.ReadAll(copyBody)
	closeErr := copyBody.Close()
	if err != nil || closeErr != nil {
		return errors.Join(err, closeErr)
	}
	if out.ContentLength != int64(len(finalBytes)) || out.Header.Get("X-Policy") != "applied" {
		return errors.New("finalizer ran before final body/header override")
	}
	hash := sha256.Sum256(finalBytes)
	out.Header.Set("X-Final-Body", hex.EncodeToString(hash[:]))
	p.finalized <- string(req.Body)
	return nil
}

func TestOverridePipelineNativeBodyAndFinalization(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(map[bool]string{false: "chat", true: "anthropic_native_body"}[native], func(t *testing.T) {
			seen := make(chan []byte, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				hash := sha256.Sum256(body)
				if r.Header.Get("X-Final-Body") != hex.EncodeToString(hash[:]) || r.Header.Get("X-Policy") != "applied" || r.Header.Get("Authorization") == "Bearer sk-test" {
					t.Error("final body/header/signing did not reach upstream through bridge")
				}
				seen <- body
				w.Header().Set("Content-Type", "application/json")
				reply := `{"choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`
				if native {
					reply = `{"id":"msg","type":"message","role":"assistant","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2}}`
				}
				_, _ = w.Write([]byte(reply))
			}))
			t.Cleanup(upstream.Close)
			var provider gateway.Provider = openai.Provider{}
			if native {
				provider = anthropic.Provider{}
			}
			inspector := &pipelineFinalizerProvider{Provider: provider, finalized: make(chan string, 1)}
			target := gateway.Target{Provider: "inspected", Secret: "upstream-key", UpstreamModel: "mapped-model",
				Settings: map[string]any{"system_prompt": "policy"}, HeaderOverride: map[string]string{"X-Policy": "applied"},
				ParamOverride: map[string]any{"temperature": 0, "native_policy": "applied"}}
			h := fixtureGateway(t, upstream.URL, target, bridge.Provider{Chat: inspector})
			body := `{"model":"alias","max_tokens":16,"temperature":0.8,"service_tier":"priority","messages":[{"role":"user","content":"hello"}]}`
			// Source-field filtering may prepare a clone before conversion, while
			// finalization must still receive the exact frozen client bytes.
			inspector.originalBody = body
			view, out := h.do(body), h.outcome()
			if view.status != 200 || !out.Charge || out.Usage.CompletionTokens != 2 || len(h.planner.results()) != 1 || <-inspector.finalized != body {
				t.Fatalf("pipeline failed or pricing input changed: view=%+v out=%+v", view, out)
			}
			actual := <-seen
			if gjson.GetBytes(actual, "model").String() != "mapped-model" || gjson.GetBytes(actual, "temperature").Float() != 0 || gjson.GetBytes(actual, "native_policy").String() != "applied" || gjson.GetBytes(actual, "service_tier").Exists() {
				t.Fatalf("native body override was not applied after conversion: %s", actual)
			}
			path := "messages.0.content"
			if native {
				path = "system"
			}
			if gjson.GetBytes(actual, path).String() != "policy" || !strings.Contains(view.body, "hello") {
				t.Fatalf("system prompt or decoded output missing: upstream=%s client=%s", actual, view.body)
			}
		})
	}
}

func TestOverridePipelineFinalizerFollowsSelectedNativeProtocol(t *testing.T) {
	seen := make(chan []byte, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		hash := sha256.Sum256(body)
		if r.URL.Path != "/v1/responses" || r.Header.Get("X-Final-Body") != hex.EncodeToString(hash[:]) {
			t.Error("bridge did not finalize selected native protocol")
		}
		seen <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"native"}]}],"usage":{"input_tokens":3,"output_tokens":2}}`))
	}))
	t.Cleanup(upstream.Close)
	inspector := &pipelineFinalizerProvider{Provider: responses.Provider{}, finalized: make(chan string, 1)}
	target := gateway.Target{Provider: "native-inspected", UpstreamModel: "mapped-model",
		Settings: map[string]any{"system_prompt": "policy"}, HeaderOverride: map[string]string{"X-Policy": "applied"},
		ParamOverride: map[string]any{"max_output_tokens": 64}}
	h := fixtureGateway(t, upstream.URL, target, bridge.Provider{Chat: openai.Provider{}, Native: map[gateway.Protocol]gateway.Provider{gateway.ProtocolResponses: inspector}})
	body := `{"model":"alias","input":"hello"}`
	request, err := http.NewRequest(http.MethodPost, h.gw.URL+"/v1/responses", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer sk-test")
	response, err := h.gw.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	result, err := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if err != nil || closeErr != nil {
		t.Fatal(errors.Join(err, closeErr))
	}
	out := h.outcome()
	if response.StatusCode != 200 || !out.Charge || out.Usage.CompletionTokens != 2 || !strings.Contains(string(result), "native") || <-inspector.finalized != body {
		t.Fatalf("selected native finalize/decode failed: status=%d out=%+v payload=%s", response.StatusCode, out, result)
	}
	actual := <-seen
	if gjson.GetBytes(actual, "instructions").String() != "policy" || gjson.GetBytes(actual, "model").String() != "mapped-model" || gjson.GetBytes(actual, "max_output_tokens").Int() != 64 {
		t.Fatalf("native override did not precede finalization: %s", actual)
	}
}
