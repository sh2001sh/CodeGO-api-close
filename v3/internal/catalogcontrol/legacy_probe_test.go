package catalogcontrol

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

const probeChatResponse = `{"choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1}}`

func TestLegacyProbeUsesProviderRequestAndActualResponse(t *testing.T) {
	for _, tc := range []struct{ provider, path, response string }{
		{"openai", "/v1/chat/completions", probeChatResponse},
		{"claude", "/v1/messages", `{"type":"message","id":"probe","role":"assistant","model":"model-a","content":[{"type":"text","text":"OK"}],"stop_reason":"end_turn","usage":{"input_tokens":2,"output_tokens":1}}`},
		{"gemini", "/v1beta/models/model-a:generateContent", `{"candidates":[{"content":{"role":"model","parts":[{"text":"OK"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":1}}`},
		{"ollama", "/api/chat", `{"model":"model-a","message":{"role":"assistant","content":"OK"},"done":true,"prompt_eval_count":2,"eval_count":1}`},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != tc.path || r.Method != http.MethodPost {
					t.Errorf("request %s %s", r.Method, r.URL.Path)
				}
				if tc.provider == "openai" && r.Header.Get("Authorization") != "Bearer fixture-secret" {
					t.Error("missing upstream authorization")
				}
				if r.Header.Get("X-Probe") != "override" || r.Header.Get("User-Agent") != "stable-probe" {
					t.Error("channel headers or credential identity were omitted")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.response))
			}))
			defer upstream.Close()
			err := runChannelProbe(context.Background(), gateway.Target{Provider: tc.provider, BaseURL: upstream.URL, Secret: "fixture-secret", UpstreamModel: "model-a", HeaderOverride: map[string]string{"X-Probe": "override"}, Fingerprint: gateway.CredentialFingerprint{UserAgent: "stable-probe"}}, "model-a", "", false)
			if err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 1 {
				t.Fatalf("upstream calls %d", calls.Load())
			}
		})
	}
}

func TestLegacyProbeRejectsFailureAndBoundaryResponses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"authentication", 401, `{"error":{"message":"fixture-secret"}}`},
		{"in_band_error", 200, `{"error":{"message":"fixture-secret"}}`},
		{"no_content", 200, `{}`},
		{"empty_body", 200, ``},
		{"invalid_json", 200, `{`},
		{"oversized", 200, strings.Repeat("x", probeBodyLimit+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer upstream.Close()
			err := runChannelProbe(context.Background(), gateway.Target{Provider: "openai", BaseURL: upstream.URL, Secret: "fixture-secret"}, "model-a", "", false)
			if err == nil {
				t.Fatal("invalid upstream response passed probe")
			}
			if strings.Contains(err.Error(), "fixture-secret") {
				t.Fatal("upstream error leaked credential")
			}
		})
	}
}

func TestLegacyProbeStreamingRequiresFinishedContent(t *testing.T) {
	for _, complete := range []bool{true, false} {
		t.Run(fmt.Sprint(complete), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"OK\"}}]}\n\n"))
				if complete {
					_, _ = w.Write([]byte("data: [DONE]\n\n"))
				}
			}))
			defer upstream.Close()
			err := runChannelProbe(context.Background(), gateway.Target{Provider: "openai", BaseURL: upstream.URL, Secret: "fixture-secret"}, "model-a", "", true)
			if (err == nil) != complete {
				t.Fatalf("complete=%v error=%v", complete, err)
			}
		})
	}
}

func TestLegacyProbeHonorsCancellationAndDoesNotFollowRedirects(t *testing.T) {
	var secondCalls atomic.Int32
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		secondCalls.Add(1)
		_, _ = w.Write([]byte(probeChatResponse))
	}))
	defer second.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, second.URL, http.StatusTemporaryRedirect)
	}))
	defer upstream.Close()
	target := gateway.Target{Provider: "openai", BaseURL: upstream.URL, Secret: "fixture-secret"}
	if err := runChannelProbe(context.Background(), target, "model-a", "", false); err == nil {
		t.Fatal("redirect probe passed")
	}
	if secondCalls.Load() != 0 {
		t.Fatal("probe followed upstream redirect")
	}
	slow := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}))
	defer slow.Close()
	target.BaseURL = slow.URL
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := runChannelProbe(ctx, target, "model-a", "", false); err == nil {
		t.Fatal("canceled probe passed")
	}
	if time.Since(start) > time.Second {
		t.Fatal("probe ignored deadline")
	}
}

func TestLegacyProbeURLAndInputValidation(t *testing.T) {
	for _, raw := range []string{"file:///etc/passwd", "javascript:1", "https://user:secret@example.test", "https://example.test/#fragment", "http://169.254.169.254", "http://0.0.0.0", "http://[fd00:ec2::254]"} {
		if _, err := validateProbeURL(raw, false); err == nil {
			t.Fatalf("unsafe probe URL accepted: %s", raw)
		}
	}
	for _, raw := range []string{"http://127.0.0.1:9000", "http://192.168.1.10", "https://example.test/v1"} {
		if _, err := validateProbeURL(raw, false); err != nil {
			t.Fatalf("configured upstream rejected: %s", raw)
		}
	}
	if _, err := probeRequest("", "", false); err == nil {
		t.Fatal("missing model accepted")
	}
	if _, err := probeRequest("model-a", "unsupported", false); err == nil {
		t.Fatal("unsupported endpoint silently replaced")
	}
}

func TestLegacyProbeRetainsResponseEndpointAliases(t *testing.T) {
	for _, endpoint := range []string{"openai-response", "responses"} {
		t.Run(endpoint, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/responses" {
					t.Errorf("unexpected response path %s", r.URL.Path)
				}
				_, _ = w.Write([]byte(`{"object":"response","id":"probe","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"OK"}]}],"usage":{"input_tokens":2,"output_tokens":1}}`))
			}))
			defer upstream.Close()
			if err := runChannelProbe(context.Background(), gateway.Target{Provider: "openai", BaseURL: upstream.URL, Secret: "fixture-secret"}, "model-a", endpoint, false); err != nil {
				t.Fatal(err)
			}
		})
	}
	request, err := probeRequest("model-a", "openai", false)
	if err != nil || request.Protocol != gateway.ProtocolOpenAIChat {
		t.Fatalf("legacy OpenAI endpoint: %v", err)
	}
}

func TestLegacyProbeProductionRoutesRequireAuthorization(t *testing.T) {
	mux := http.NewServeMux()
	New(nil, nil, nil).Register(mux, nil)
	for _, endpoint := range []struct{ method, path string }{
		{"GET", "/api/channel/test"}, {"GET", "/api/channel/test/1"}, {"GET", "/api/channel/fetch_models/1"},
		{"POST", "/api/channel/fetch_models"}, {"GET", "/api/channel/models"}, {"GET", "/api/channel/models_enabled"},
	} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(endpoint.method, endpoint.path, strings.NewReader(`{}`)))
		if w.Code != 403 {
			t.Fatalf("%s %s status %d", endpoint.method, endpoint.path, w.Code)
		}
	}
}

func TestLegacyProbeUsesRuntimeOverridesAndRejectsInvalidConfig(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"temperature":0`) || r.Header.Get("X-Saved") != "yes" {
			t.Error("runtime override was lost in probe")
		}
		_, _ = w.Write([]byte(probeChatResponse))
	}))
	defer upstream.Close()
	target := gateway.Target{Provider: "openai", BaseURL: upstream.URL, Secret: "fixture-only", ParamOverride: map[string]any{"temperature": 0}, HeaderOverride: map[string]string{"X-Saved": "yes"}}
	if err := runChannelProbe(context.Background(), target, "gpt", "", false); err != nil {
		t.Fatal(err)
	}
	target.HeaderOverride = map[string]string{"X-Invalid": "value\r\nInjected: x"}
	if err := runChannelProbe(context.Background(), target, "gpt", "", false); err == nil || calls.Load() != 1 {
		t.Fatal("invalid header override contacted upstream")
	}
}
