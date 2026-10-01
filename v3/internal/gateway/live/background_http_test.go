package live

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestBackgroundOwnerAndKeyIsolation(t *testing.T) {
	var called atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called.Add(1)
		_, _ = io.WriteString(w, `{"id":"resp_123","status":"completed"}`)
	}))
	defer up.Close()
	_, mux, repo, _ := backgroundFixture(t, gateway.Target{BaseURL: up.URL})
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		path := "/v1/responses/resp_123"
		if method == http.MethodPost {
			path += "/cancel"
		}
		for _, item := range []struct {
			key    string
			status int
		}{{"", 401}, {"bad", 401}, {"unavailable", 503}, {"other-key", 404}, {"other-user", 404}} {
			w := backgroundCall(mux, method, path, item.key)
			if w.Code != item.status {
				t.Errorf("%s key=%q: status %d, want %d", method, item.key, w.Code, item.status)
			}
		}
	}
	// Check the handler itself enforces owner/key even if a repository returns
	// another owner's locator by mistake.
	repo.unsafe = true
	for _, key := range []string{"other-key", "other-user"} {
		if w := backgroundCall(mux, http.MethodGet, "/v1/responses/resp_123", key); w.Code != 404 {
			t.Errorf("unsafe repository key=%s: status %d", key, w.Code)
		}
	}
	if called.Load() != 0 {
		t.Fatal("unauthorized request reached upstream")
	}
}

func TestBackgroundAliasesAndCancellation(t *testing.T) {
	var paths []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer upstream-secret" {
			t.Error("wrong upstream credentials")
		}
		if r.Header.Get("Cookie") != "" || r.URL.Query().Get("channel_id") != "" || r.URL.Query().Get("url") != "" {
			t.Error("caller routing or cookies were forwarded")
		}
		body, _ := io.ReadAll(r.Body)
		if len(body) != 0 {
			t.Error("retrieval/cancel body must be empty")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Set-Cookie", "secret=upstream")
		_, _ = io.WriteString(w, `{"id":"resp_123","status":"cancelled"}`)
	}))
	defer up.Close()
	_, mux, repo, limits := backgroundFixture(t, gateway.Target{BaseURL: up.URL + "/v1"})
	for _, alias := range []string{"/v1/responses", "/responses", "/backend-api/codex/responses"} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			path := alias + "/resp_123"
			if method == http.MethodPost {
				path += "/cancel"
			}
			w := backgroundCall(mux, method, path+"?channel_id=999&url=https://evil.invalid", "owner")
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"cancelled"`) {
				t.Errorf("%s %s: status=%d body=%s", method, path, w.Code, w.Body.String())
			}
			if w.Header().Get("Set-Cookie") != "" {
				t.Error("upstream cookie leaked")
			}
		}
	}
	if len(paths) != 6 || limits.acquires != 6 || limits.releases != 6 {
		t.Fatalf("paths=%v limits=%+v", paths, limits)
	}
	for i, path := range paths {
		want := "GET /v1/responses/resp_123"
		if i%2 == 1 {
			want = "POST /v1/responses/resp_123/cancel"
		}
		if path != want {
			t.Errorf("path=%s want=%s", path, want)
		}
	}
	if repo.locator.ID != "resp_123" {
		t.Fatal("cancellation discarded the response locator")
	}
}

func TestBackgroundNativeEndpoints(t *testing.T) {
	for _, item := range []struct {
		provider, base, secret, path, version, auth, apiKey, account string
	}{
		{provider: "openai", base: "https://example.test", secret: "key", path: "/v1/responses/resp_123", auth: "Bearer key"},
		{provider: "responses", base: "https://example.test/prefix/v1", secret: "key", path: "/prefix/v1/responses/resp_123", auth: "Bearer key"},
		{provider: "openaimax", base: "https://example.test/v1/responses?tenant=1", secret: "key", path: "/v1/responses/resp_123", auth: "Bearer key"},
		{provider: "azure", base: "https://example.openai.azure.com", secret: "azure-key", path: "/openai/v1/responses/resp_123", version: "preview", apiKey: "azure-key"},
		{provider: "azure", base: "https://example.cognitiveservices.azure.com?api-version=custom", secret: "azure-key", path: "/openai/responses/resp_123", version: "custom", apiKey: "azure-key"},
		{provider: "codex", base: "https://chatgpt.test/backend-api/codex", secret: `{"access_token":"access","account_id":"account"}`, path: "/backend-api/codex/responses/resp_123", auth: "Bearer access", account: "account"},
	} {
		t.Run(item.provider+item.base, func(t *testing.T) {
			req := &gateway.Request{Body: []byte(`{}`), Model: "gpt-test", Protocol: gateway.ProtocolResponses, Stream: true}
			query, _ := backgroundQuery("stream=true&starting_after=42&api-version=evil", true)
			out, err := backgroundRequest(context.Background(), req, gateway.Target{Provider: item.provider, BaseURL: item.base, Secret: item.secret}, "resp_123", http.MethodGet, query)
			if err != nil {
				t.Fatal(err)
			}
			if out.URL.Path != item.path || out.Method != "GET" || out.Body != nil || out.ContentLength != 0 {
				t.Fatalf("wrong request: %+v", out)
			}
			if out.URL.Query().Get("api-version") != item.version || out.URL.Query().Get("starting_after") != "42" || out.URL.Query().Get("stream") != "true" {
				t.Errorf("wrong query %s", out.URL.RawQuery)
			}
			if out.Header.Get("Authorization") != item.auth || out.Header.Get("Api-Key") != item.apiKey || out.Header.Get("Chatgpt-Account-Id") != item.account {
				t.Error("incorrect native authentication")
			}
			if out.Header.Get("Accept") != "text/event-stream" {
				t.Error("missing stream accept header")
			}
			if strings.Contains(item.base, "tenant=1") && out.URL.Query().Get("tenant") != "1" {
				t.Error("trusted endpoint query lost")
			}
		})
	}
}
