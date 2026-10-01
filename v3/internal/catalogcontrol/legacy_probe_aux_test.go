package catalogcontrol

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestAuxiliaryProbesUseActualAdaptersAndRejectFailures(t *testing.T) {
	for _, c := range []struct{ provider, endpoint, path, response string }{
		{"openai", "embeddings", "/v1/embeddings", `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":1,"total_tokens":1}}`},
		{"jina", "jina-rerank", "/v1/rerank", `{"results":[{"index":0,"relevance_score":0.9}],"usage":{"total_tokens":1}}`},
		{"openai", "image-generation", "/v1/images/generations", `{"created":1,"data":[{"b64_json":"ZmFrZQ=="}]}`},
		{"openai", "openai-response-compact", "/v1/responses/compact", `{"object":"response.compaction","output":[{"type":"compaction","encrypted_content":"fixture"}],"usage":{"input_tokens":1,"output_tokens":1}}`},
	} {
		t.Run(c.endpoint, func(t *testing.T) {
			for _, result := range []struct {
				name   string
				status int
				body   string
				valid  bool
			}{
				{"success", 200, c.response, true},
				{"authentication", 401, `{"error":"fixture-only-secret"}`, false},
				{"missing_output", 200, `{}`, false},
				{"empty_output", 200, `{"data":[],"results":[],"output":[]}`, false},
				{"truncated", 200, `{"data":[`, false},
			} {
				up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != c.path {
						t.Errorf("unexpected probe path %s", r.URL.Path)
					}
					if r.Header.Get("User-Agent") != "stable-probe" || r.Header.Get("X-Saved") != "yes" {
						t.Error("auxiliary probe omitted saved headers or credential fingerprint")
					}
					w.WriteHeader(result.status)
					_, _ = w.Write([]byte(result.body))
				}))
				err := runChannelProbe(context.Background(), gateway.Target{Provider: c.provider, BaseURL: up.URL, Secret: "fixture-only-secret", UpstreamModel: "test-model",
					HeaderOverride: map[string]string{"X-Saved": "yes"}, Fingerprint: gateway.CredentialFingerprint{UserAgent: "stable-probe"}}, "test-model", c.endpoint, false)
				up.Close()
				if (err == nil) != result.valid {
					t.Fatalf("%s valid=%v error=%v", result.name, result.valid, err)
				}
				if err != nil && strings.Contains(err.Error(), "fixture-only-secret") {
					t.Fatal("auxiliary error leaked credential")
				}
			}
		})
	}
}
