package auxiliary

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func TestVectorNativeWireRequests(t *testing.T) {
	cases := []struct {
		provider                 string
		op                       Operation
		body, path, secret, auth string
		fields                   map[string]string
		absent                   []string
	}{
		{"ollama", Embeddings, `{"model":"client","input":["one","two"],"dimensions":2,"temperature":0.1,"seed":7}`, "/api/embed", "key", "Bearer key", map[string]string{"model": "mapped", "input.0": "one", "options.dimensions": "2", "dimensions": "2", "options.temperature": "0.1", "options.seed": "7"}, []string{"temperature", "seed"}},
		{"cohere", Rerank, `{"query":"question","documents":["one",{"text":"two"}]}`, "/v1/rerank", "key", "Bearer key", map[string]string{"model": "mapped", "top_n": "1", "return_documents": "true", "documents.1.text": "two"}, nil},
		{"cohere", Rerank, `{"query":"question","documents":["one"],"return_documents":false,"top_n":2}`, "/v1/rerank", "key", "Bearer key", map[string]string{"return_documents": "false", "top_n": "2"}, nil},
		{"ali", Rerank, `{"query":"question","documents":["one"],"top_n":1}`, "/api/v1/services/rerank/text-rerank/text-rerank", "key", "Bearer key", map[string]string{"model": "mapped", "input.query": "question", "parameters.top_n": "1", "parameters.return_documents": "true"}, []string{"query", "documents"}},
		{"ali", Embeddings, `{"input":"one","dimensions":2}`, "/compatible-mode/v1/embeddings", "key", "Bearer key", map[string]string{"model": "mapped", "input": "one", "dimensions": "2"}, nil},
		{"jina", Embeddings, `{"input":[{"image":"url"}],"task":"retrieval.query","encoding_format":"base64"}`, "/v1/embeddings", "key", "Bearer key", map[string]string{"model": "mapped", "input.0.image": "url", "task": "retrieval.query"}, []string{"encoding_format"}},
		{"jina", Rerank, `{"query":"question","documents":["one"],"return_documents":false}`, "/v1/rerank", "key", "Bearer key", map[string]string{"return_documents": "false"}, nil},
		{"cloudflare", Embeddings, `{"input":["one"],"dimensions":2}`, "/client/v4/accounts/account/ai/v1/embeddings", "account|key", "Bearer key", map[string]string{"model": "mapped", "dimensions": "2"}, nil},
		{"zhipu_4v", Embeddings, `{"input":"one"}`, "/api/paas/v4/embeddings", "key", "Bearer key", map[string]string{"model": "mapped"}, nil},
		{"mokaai", Embeddings, `{"input":["one"]}`, "/embeddings", "key", "Bearer key", map[string]string{"model": "mapped"}, nil},
		{"mokaai", Embeddings, `{"input":"one"}`, "/embeddings", "key", "Bearer key", map[string]string{"model": "mapped", "input.0": "one"}, nil},
		{"volcengine", Embeddings, `{"input":"one"}`, "/api/v3/embeddings", "key", "Bearer key", map[string]string{"model": "mapped"}, nil},
		{"baidu", Embeddings, `{"input":"one"}`, "/rpc/2.0/ai_custom/v1/wenxinworkshop/embeddings/bge_large_zh", "access-token", "", map[string]string{"input.0": "one"}, []string{"model"}},
	}
	for _, tc := range cases {
		t.Run(tc.provider+"/"+string(tc.op)+"/"+tc.body, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != tc.path {
					t.Errorf("wire route = %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("Authorization") != tc.auth {
					t.Errorf("wire auth mismatch")
				}
				if r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("content type = %q", r.Header.Get("Content-Type"))
				}
				if tc.provider == "baidu" && r.URL.Query().Get("access_token") != "access-token" {
					t.Error("missing Baidu OAuth token")
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatal(err)
				}
				for field, want := range tc.fields {
					if got := gjson.GetBytes(body, field).String(); got != want {
						t.Errorf("%s = %q, want %q; %s", field, got, want, body)
					}
				}
				for _, field := range tc.absent {
					if gjson.GetBytes(body, field).Exists() {
						t.Errorf("unconverted field %s", field)
					}
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			model := "mapped"
			if tc.provider == "baidu" {
				model = "bge-large-zh"
			}
			req := &gateway.Request{Model: "client", Body: []byte(`{"billing_only":true}`)}
			target := gateway.Target{BaseURL: server.URL, Secret: tc.secret, UpstreamModel: model}
			wire, err := vectorAdapters()[tc.provider].Build(context.Background(), req, target, Input{Operation: tc.op, Body: []byte(tc.body)})
			if err != nil {
				t.Fatal(err)
			}
			response, err := server.Client().Do(wire)
			if err != nil {
				t.Fatal(err)
			}
			_ = response.Body.Close()
			if calls != 1 {
				t.Fatalf("wire calls = %d", calls)
			}
			if string(req.Body) != `{"billing_only":true}` {
				t.Error("billing body was mutated")
			}
		})
	}
}

func TestVectorRejectsUnsupportedOperations(t *testing.T) {
	supported := map[string]map[Operation]bool{
		"ollama": {Embeddings: true}, "cohere": {Rerank: true}, "ali": {Embeddings: true, Rerank: true}, "jina": {Embeddings: true, Rerank: true}, "cloudflare": {Embeddings: true, Transcriptions: true, Translations: true, Completions: true}, "baidu": {Embeddings: true}, "zhipu_4v": {Embeddings: true}, "mokaai": {Embeddings: true}, "volcengine": {Embeddings: true},
	}
	operations := []Operation{Embeddings, Rerank, Images, ImageEdits, Edits, Transcriptions, Translations, Speech, Moderations, Completions, Compact, Search, GeminiEmbed, GeminiBatchEmbed, GeminiImages}
	for name, adapter := range vectorAdapters() {
		for _, operation := range operations {
			if supported[name][operation] {
				continue
			}
			_, err := adapter.Build(context.Background(), nil, gateway.Target{}, Input{Operation: operation})
			var typed *gateway.UpstreamError
			if !errors.As(err, &typed) || typed.Code != "unsupported_operation" {
				t.Errorf("%s %s error = %v", name, operation, err)
			}
			_, err = adapter.Decode(context.Background(), nil, gateway.Target{}, Input{Operation: operation}, nil)
			if !errors.As(err, &typed) || typed.Code != "unsupported_operation" {
				t.Errorf("%s %s decode error = %v", name, operation, err)
			}
		}
	}
}

func TestVectorRejectsMalformedNativeInput(t *testing.T) {
	cases := []struct {
		provider string
		op       Operation
		body     string
	}{
		{"ollama", Embeddings, `{"input":[]}`}, {"ollama", Embeddings, `{"input":[1,2]}`}, {"ollama", Embeddings, `{"input":"text","encoding_format":"base64"}`}, {"ollama", Embeddings, `{"input":"text","options":[]}`}, {"ollama", Embeddings, `{"input":"text","user":"unsupported"}`},
		{"baidu", Embeddings, `{"input":["text",1]}`}, {"baidu", Embeddings, `{"input":"text","dimensions":128}`}, {"baidu", Embeddings, `{"input":"text","encoding_format":"base64"}`},
		{"ali", Rerank, `{"query":"text","documents":[]}`}, {"ali", Rerank, `{"query":"text","documents":["a"],"top_n":0}`}, {"ali", Rerank, `{"query":"text","documents":["a"],"return_documents":"yes"}`}, {"ali", Rerank, `{"query":"text","documents":["a"],"overlap_tokens":10}`},
		{"cohere", Rerank, `{"query":"","documents":["a"]}`}, {"cohere", Rerank, `{"query":"text","documents":["a"],"top_n":1.5}`}, {"volcengine", Embeddings, `{"input":null}`}, {"zhipu_4v", Embeddings, `[]`},
	}
	for _, tc := range cases {
		_, err := vectorAdapters()[tc.provider].Build(context.Background(), &gateway.Request{Model: "model"}, gateway.Target{BaseURL: "https://invalid.example", Secret: "key"}, Input{Operation: tc.op, Body: []byte(tc.body)})
		if err == nil {
			t.Errorf("%s accepted %s", tc.provider, tc.body)
		}
	}
}

func TestVectorProviderBaseNormalization(t *testing.T) {
	cases := []struct{ provider, base, path, secret string }{
		{"ollama", "/v1", "/api/embed", "key"}, {"ollama", "/api", "/api/embed", "key"}, {"ali", "/compatible-mode/v1", "/compatible-mode/v1/embeddings", "key"},
		{"zhipu_4v", "/api/paas/v4", "/api/paas/v4/embeddings", "key"}, {"zhipu_4v", "/api/coding/paas/v4", "/api/coding/paas/v4/embeddings", "key"},
		{"volcengine", "/api/v3", "/api/v3/embeddings", "key"}, {"jina", "/v1", "/v1/embeddings", "key"},
		{"cloudflare", "/client/v4", "/client/v4/accounts/account/ai/v1/embeddings", `{"account_id":"account","api_key":"key"}`},
		{"cloudflare", "/client/v4/accounts/account/ai", "/client/v4/accounts/account/ai/v1/embeddings", "key"},
	}
	for _, tc := range cases {
		wire, err := vectorAdapters()[tc.provider].Build(context.Background(), &gateway.Request{Model: "model"}, gateway.Target{BaseURL: "https://api.example" + tc.base, Secret: tc.secret}, Input{Operation: Embeddings, Body: []byte(`{"input":"text"}`)})
		if err != nil {
			t.Errorf("%s %s: %v", tc.provider, tc.base, err)
			continue
		}
		if wire.URL.Path != tc.path {
			t.Errorf("%s path = %s, want %s", tc.provider, wire.URL.Path, tc.path)
		}
		_ = wire.Body.Close()
	}
}

func TestCloudflareCredentialValidation(t *testing.T) {
	for _, secret := range []string{"", "key", "bad/account|token", "account|", "account|key|extra", `{"account_id":"account"}`, `{`} {
		_, _, err := vectorCloudflareBase("https://api.cloudflare.com", secret)
		if err == nil {
			t.Errorf("accepted invalid credentials %q", secret)
		}
	}
	base, token, err := vectorCloudflareBase("https://api.cloudflare.com", `{"account_id":"account","token":"key"}`)
	if err != nil || token != "key" || !strings.HasSuffix(base, "/client/v4/accounts/account/ai") {
		t.Fatalf("JSON credentials: %s %s %v", base, token, err)
	}
}
