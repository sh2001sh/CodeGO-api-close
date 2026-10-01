package providers_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/auxiliary"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers"
	"github.com/tidwall/gjson"
)

// These v2 channel types have no Chat capability. Acceptance uses their
// actual operations through the billed auxiliary HTTP pipeline instead.
func TestProviderReplayNonChatOperations(t *testing.T) {
	cases := []struct {
		name, id, model, path, nativePath, input, output string
		actual                                           bool
	}{
		{"jina_embeddings", "jina", "jina-embeddings-v3", "/v1/embeddings", "/v1/embeddings", `{"model":"public-alias","input":["hello"],"task":"retrieval.query","encoding_format":"base64"}`, `{"data":[{"index":0,"embedding":[0.25,0.5]}],"usage":{"total_tokens":10}}`, true},
		{"jina_embeddings_missing_usage", "jina", "jina-embeddings-v3", "/v1/embeddings", "/v1/embeddings", `{"model":"public-alias","input":["hello"]}`, `{"data":[{"index":0,"embedding":[0.25,0.5]}]}`, false},
		{"jina_rerank", "jina", "jina-reranker-v2-base-multilingual", "/v1/rerank", "/v1/rerank", `{"model":"public-alias","query":"hello","documents":["hello world"]}`, `{"results":[{"index":0,"relevance_score":0.9}],"usage":{"total_tokens":10}}`, true},
		{"replicate_images", "replicate", "black-forest-labs/flux-1.1-pro", "/v1/images/generations", "/v1/models/black-forest-labs/flux-1.1-pro/predictions", `{"model":"public-alias","prompt":"hello","n":1,"response_format":"url"}`, `{"id":"p1","status":"succeeded","output":["https://images.example/hello.png"]}`, false},
		{"mokaai_embeddings", "mokaai", "m3e-large", "/v1/embeddings", "/embeddings", `{"model":"public-alias","input":"hello"}`, `{"data":[{"index":0,"embedding":[0.25,0.5]}],"usage":{"prompt_tokens":10,"completion_tokens":0,"total_tokens":10}}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != tc.nativePath {
					t.Errorf("native operation destination=%s %s; want POST %s", r.Method, r.URL.Path, tc.nativePath)
				}
				assertHeader(t, r, "Authorization", "Bearer test-token")
				body, _ := io.ReadAll(r.Body)
				switch tc.id {
				case "jina":
					assertJSON(t, body, "model", tc.model)
					forbidJSON(t, body, "encoding_format")
				case "mokaai":
					assertJSON(t, body, "model", tc.model)
					assertJSON(t, body, "input.0", "hello")
				default:
					assertJSON(t, body, "input.prompt", "hello")
					assertJSON(t, body, "input.num_outputs", "1")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.output)
			}))
			t.Cleanup(upstream.Close)
			planner := &replayPlanner{targets: []gateway.Target{{ChannelID: 1, Provider: tc.id, BaseURL: upstream.URL, Secret: "test-token", UpstreamModel: tc.model}}}
			settler := &replaySettler{outcomes: make(chan gateway.Outcome, 1)}
			h, err := auxiliary.New(auxiliary.Config{Authorizer: replayAuth{}, Planner: planner, Settler: settler, RelayTimeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			h.Register(mux)
			server := httptest.NewServer(mux)
			t.Cleanup(server.Close)
			request, _ := http.NewRequest("POST", server.URL+tc.path, strings.NewReader(tc.input))
			request.Header.Set("Authorization", "Bearer replay-key")
			request.Header.Set("Content-Type", "application/json")
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if response.StatusCode != 200 {
				t.Fatalf("native operation unavailable: status=%d body=%s", response.StatusCode, body)
			}
			out := <-settler.outcomes
			want := gateway.TerminalCompletedNoUsage
			if tc.actual {
				want = gateway.TerminalCompleted
			}
			if out.Terminal != want || !out.Charge || out.Usage.Estimated == tc.actual {
				t.Errorf("operation outcome=%s charge=%v usage=%+v; want %s", out.Terminal, out.Charge, out.Usage, want)
			}
			if tc.actual && out.Usage.PromptTokens != 10 {
				t.Errorf("actual native vector usage=%+v", out.Usage)
			}
			if tc.id == "replicate" && gjson.GetBytes(body, "data.0.url").Str != "https://images.example/hello.png" {
				t.Errorf("native image result lost: %s", body)
			}
		})
	}
}

func TestProviderReplayNonChatChannelsRejectChat(t *testing.T) {
	for _, id := range []string{"jina", "replicate"} {
		t.Run(id, func(t *testing.T) {
			p := providers.Registry()[id]
			if p == nil {
				t.Fatalf("provider %s missing", id)
			}
			_, err := p.BuildRequest(context.Background(), &gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Model: "m", Body: []byte(`{"messages":[{"role":"user","content":"hello"}]}`)}, gateway.Target{})
			var typed *gateway.UpstreamError
			if !errors.As(err, &typed) || typed.Status != 400 || typed.Code != "unsupported_protocol" {
				t.Errorf("non-Chat provider accepted Chat or lost explicit unsupported error: %v", err)
			}
		})
	}
}
