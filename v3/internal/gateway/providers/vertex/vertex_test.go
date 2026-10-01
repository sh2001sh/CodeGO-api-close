package vertex

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func encoded(t *testing.T, c Credentials) string {
	t.Helper()
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestNativeAndChatRequests(t *testing.T) {
	for _, tc := range []struct {
		name, client, model, body, path string
		protocol                        gateway.Protocol
		stream                          bool
	}{
		{"claude-native", "claude-3-5-sonnet-20241022", "", `{"model":"claude-3-5-sonnet-20241022","messages":[{"role":"user","content":"hello"}],"max_tokens":32,"thinking":{"type":"enabled","budget_tokens":16}}`, "/v1/projects/p-one/locations/us-east5/publishers/anthropic/models/claude-3-5-sonnet-v2@20241022:rawPredict", gateway.ProtocolAnthropic, false},
		{"claude-chat-stream-alias", "alias", "claude-sonnet-4-20250514", `{"model":"alias","stream":true,"messages":[{"role":"system","content":"be brief"},{"role":"user","content":"hello"}],"max_tokens":32}`, "/v1/projects/p-one/locations/us-east5/publishers/anthropic/models/claude-sonnet-4@20250514:streamRawPredict", gateway.ProtocolOpenAIChat, true},
		{"gemini-native", "gemini-2.5-pro", "", `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"temperature":0.2}}`, "/v1/projects/p-one/locations/us-east5/publishers/google/models/gemini-2.5-pro:generateContent", gateway.ProtocolGemini, false},
		{"gemini-chat-stream", "models/gemini-2.5-flash", "", `{"model":"models/gemini-2.5-flash","stream":true,"messages":[{"role":"user","content":"hello"}],"temperature":0.4}`, "/v1/projects/p-one/locations/us-east5/publishers/google/models/gemini-2.5-flash:streamGenerateContent", gateway.ProtocolOpenAIChat, true},
		{"open-source", "alias", "meta/llama-3.3-70b-instruct-maas", `{"model":"alias","messages":[{"role":"user","content":"hello"}]}`, "/v1beta1/projects/p-one/locations/us-east5/endpoints/openapi/chat/completions", gateway.ProtocolOpenAIChat, false},
		{"llama", "llama-3.3-70b-instruct-maas", "", `{"model":"llama-3.3-70b-instruct-maas","messages":[{"role":"user","content":"hello"}]}`, "/v1beta1/projects/p-one/locations/us-east5/endpoints/openapi/chat/completions", gateway.ProtocolOpenAIChat, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := &gateway.Request{Model: tc.client, Body: []byte(tc.body), Protocol: tc.protocol, Stream: tc.stream,
				ClientHeaders: map[string]string{"anthropic-beta": "test-beta", "Authorization": "client-secret"}}
			target := gateway.Target{BaseURL: "https://proxy.example/prefix/v1?custom=yes", UpstreamModel: tc.model,
				Secret: encoded(t, Credentials{ProjectID: "p-one", Region: "us-east5", AccessToken: "token-one"})}
			out, err := (Provider{}).BuildRequest(context.Background(), req, target)
			if tc.path == "" {
				if err == nil {
					t.Fatal("invalid model path accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = out.Body.Close() }()
			if out.URL.Path != "/prefix"+tc.path || out.URL.Query().Get("custom") != "yes" {
				t.Fatalf("wrong URL: %s", out.URL)
			}
			if out.Header.Get("Authorization") != "Bearer token-one" || out.Header.Get("X-Goog-User-Project") != "p-one" || out.Header.Get("X-Api-Key") != "" || out.Header.Get("X-Goog-Api-Key") != "" {
				t.Fatalf("incorrect auth headers: %+v", out.Header)
			}
			if tc.stream && (out.URL.Query().Get("alt") != "sse" || out.Header.Get("Accept") != "text/event-stream") {
				t.Fatal("missing streaming parameters")
			}
			body, _ := io.ReadAll(out.Body)
			if tc.model == "meta/llama-3.3-70b-instruct-maas" && gjson.GetBytes(body, "model").Str != tc.model {
				t.Fatalf("OpenAPI publisher model was not preserved: %s", body)
			}
			if strings.Contains(tc.path, "/anthropic/") {
				if gjson.GetBytes(body, "model").Exists() || gjson.GetBytes(body, "anthropic_version").Str != "vertex-2023-10-16" || out.Header.Get("Anthropic-Version") != "" || out.Header.Get("Anthropic-Beta") != "test-beta" {
					t.Fatalf("wrong Vertex Anthropic payload: %s %+v", body, out.Header)
				}
				if gjson.GetBytes(body, "messages.0.content").Raw == "" {
					t.Fatalf("lost messages: %s", body)
				}
			} else if strings.Contains(tc.path, "/google/") && !gjson.GetBytes(body, "contents.0.parts.0.text").Exists() {
				t.Fatalf("lost Gemini contents: %s", body)
			}
			if string(req.Body) != tc.body {
				t.Fatal("mutated original request")
			}
		})
	}
}

func TestEndpointRegionsAndAuthentication(t *testing.T) {
	for _, tc := range []struct {
		name, secret, host, path string
	}{
		{"plain-api-key", "my-api-key", "aiplatform.googleapis.com", "/v1/publishers/google/models/gemini:generateContent"},
		{"global-sa-shape-bearer", encoded(t, Credentials{ProjectID: "project", AccessToken: "t"}), "aiplatform.googleapis.com", "/v1/projects/project/locations/global/publishers/google/models/gemini:generateContent"},
		{"model-region", encoded(t, Credentials{ProjectID: "project", Region: "us-west1", Regions: map[string]string{"alias": "europe-west4", "default": "us-east1"}, APIKey: "key"}), "europe-west4-aiplatform.googleapis.com", "/v1/projects/project/locations/europe-west4/publishers/google/models/gemini:generateContent"},
		{"default-region", encoded(t, Credentials{Regions: map[string]string{"default": "us-east1"}, APIKey: "key"}), "us-east1-aiplatform.googleapis.com", "/v1/publishers/google/models/gemini:generateContent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := (Provider{}).BuildRequest(context.Background(), &gateway.Request{Model: "alias", Protocol: gateway.ProtocolGemini, Body: []byte(`{"contents":[]}`)}, gateway.Target{Secret: tc.secret, UpstreamModel: "gemini"})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = out.Body.Close() }()
			if out.URL.Host != tc.host || out.URL.Path != tc.path {
				t.Fatalf("wrong routing: %s", out.URL)
			}
			if tc.name != "global-sa-shape-bearer" && (out.URL.Query().Get("key") == "" || out.Header.Get("Authorization") != "") {
				t.Fatal("API key was not routed in query")
			}
		})
	}
}

func TestRejectProtocolAndPathBeforeOAuth(t *testing.T) {
	for _, tc := range []struct {
		model, base, secret string
		protocol            gateway.Protocol
	}{
		{"gemini", "", `{"project_id":"p","private_key":"not-a-key","client_email":"e"}`, gateway.ProtocolAnthropic},
		{"claude-sonnet-4", "", "key", gateway.ProtocolGemini},
		{"llama-3", "", "key", gateway.ProtocolGemini},
		{"gemini", "", "key", gateway.ProtocolResponses},
		{"../model", "", "key", gateway.ProtocolOpenAIChat},
		{"gemini", "ftp://host", "key", gateway.ProtocolGemini},
		{"gemini", "https://user:password@host", "key", gateway.ProtocolGemini},
		{"llama-3", "", "key", gateway.ProtocolOpenAIChat},
	} {
		_, err := (Provider{}).BuildRequest(context.Background(), &gateway.Request{Model: tc.model, Protocol: tc.protocol, Body: []byte(`{}`)}, gateway.Target{BaseURL: tc.base, Secret: tc.secret})
		if err == nil {
			t.Fatalf("accepted invalid request: %+v", tc)
		}
	}
}

func TestRequestContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err := (Provider{}).BuildRequest(ctx, &gateway.Request{Model: "gemini", Protocol: gateway.ProtocolGemini, Received: time.Now()}, gateway.Target{Secret: "key"})
	if out != nil || err != context.Canceled {
		t.Fatalf("cancelled request accepted: %+v %v", out, err)
	}
}

var _ gateway.Provider = Provider{}
