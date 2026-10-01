package catalogcontrol

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestLegacyModelDiscoveryUsesProviderEndpoints(t *testing.T) {
	for _, tc := range []struct {
		provider, basePath, path, header, body string
		want                                   []string
	}{
		{"openai", "/tenant/v1", "/tenant/v1/models", "Authorization", `{"data":[{"id":"b"},{"id":"a"},{"id":"a"}]}`, []string{"a", "b"}},
		{"gemini", "/v1beta", "/v1beta/models", "x-goog-api-key", `{"models":[{"name":"models/gemini-a"}]}`, []string{"gemini-a"}},
		{"ollama", "", "/api/tags", "Authorization", `{"models":[{"name":"local-a:latest"}]}`, []string{"local-a:latest"}},
		{"claude", "", "/v1/models", "x-api-key", `{"data":[]}`, []string{}},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.path || r.Method != http.MethodGet {
					t.Errorf("request %s %s", r.Method, r.URL.Path)
				}
				if !strings.Contains(r.Header.Get(tc.header), "fixture-secret") {
					t.Error("credential missing from upstream request")
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer upstream.Close()
			models, err := fetchProbeModels(context.Background(), gateway.Target{Provider: tc.provider, BaseURL: upstream.URL + tc.basePath, Secret: "fixture-secret"})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(models, tc.want) {
				t.Fatalf("models %v, want %v", models, tc.want)
			}
		})
	}
}

func TestLegacyModelDiscoveryRejectsFailuresWithoutLeakingSecrets(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"refused", 401, `{"error":"fixture-secret"}`},
		{"invalid", 200, `{`},
		{"missing_models", 200, `{}`},
		{"secret_echo", 200, `{"data":[{"id":"fixture-secret"}]}`},
		{"invalid_identifier", 200, `{"data":[{"id":"bad\nname"}]}`},
		{"oversized", 200, strings.Repeat("x", probeBodyLimit+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer upstream.Close()
			_, err := fetchProbeModels(context.Background(), gateway.Target{Provider: "openai", BaseURL: upstream.URL, Secret: "fixture-secret"})
			if err == nil {
				t.Fatal("invalid model response accepted")
			}
			if strings.Contains(err.Error(), "fixture-secret") {
				t.Fatal("upstream credential leaked")
			}
		})
	}
}
