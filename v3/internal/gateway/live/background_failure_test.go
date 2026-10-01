package live

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestBackgroundStoreResolverAndLimitsFailClosed(t *testing.T) {
	var called atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called.Add(1) }))
	defer up.Close()
	for _, item := range []struct {
		name   string
		status int
	}{{"store", 503}, {"resolver", 503}, {"wrong-route", 503}, {"unsupported", 502}, {"invalid-base", 502}, {"limit", 429}, {"limit-store", 503}} {
		t.Run(item.name, func(t *testing.T) {
			h, mux, repo, limits := backgroundFixture(t, gateway.Target{BaseURL: up.URL})
			switch item.name {
			case "store":
				repo.err = errors.New("redis unavailable")
			case "resolver":
				h.cfg.Resolve = func(context.Context, int64, int64) (gateway.Target, error) {
					return gateway.Target{}, errors.New("snapshot unavailable")
				}
			case "wrong-route":
				h.cfg.Resolve = func(context.Context, int64, int64) (gateway.Target, error) {
					return gateway.Target{ChannelID: 99, CredentialID: 9, Provider: "openai", BaseURL: up.URL}, nil
				}
			case "unsupported":
				h.cfg.Resolve = func(context.Context, int64, int64) (gateway.Target, error) {
					return gateway.Target{ChannelID: 7, CredentialID: 9, Provider: "anthropic", BaseURL: up.URL}, nil
				}
			case "invalid-base":
				h.cfg.Resolve = func(context.Context, int64, int64) (gateway.Target, error) {
					return gateway.Target{ChannelID: 7, CredentialID: 9, Provider: "openai", BaseURL: "file:///tmp/secret"}, nil
				}
			case "limit":
				limits.err = gateway.ErrRateLimited
			case "limit-store":
				limits.err = gateway.ErrLimitsUnavailable
			}
			w := backgroundCall(mux, "GET", "/v1/responses/resp_123", "owner")
			if w.Code != item.status {
				t.Errorf("status=%d want=%d body=%s", w.Code, item.status, w.Body.String())
			}
			if limits.releases != 0 {
				t.Error("release called without acquired lease")
			}
		})
	}
	if called.Load() != 0 {
		t.Fatal("failed request reached upstream")
	}
}

func TestBackgroundIDValidation(t *testing.T) {
	for _, id := range []string{"", "..", "resp/123", "resp?123", "resp#123", "resp%2f123", "resp 123", strings.Repeat("a", 257)} {
		if backgroundID(id) {
			t.Errorf("unsafe ID accepted: %q", id)
		}
	}
	for _, id := range []string{"resp_123", "12345678-1234-1234-1234-123456789abc"} {
		if !backgroundID(id) {
			t.Errorf("valid ID rejected: %s", id)
		}
	}
}

func TestBackgroundTimeoutReleasesLease(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer up.Close()
	h, mux, _, limits := backgroundFixture(t, gateway.Target{BaseURL: up.URL})
	h.cfg.SessionTimeout = 20 * time.Millisecond
	w := backgroundCall(mux, "GET", "/v1/responses/resp_123", "owner")
	if w.Code != 504 || limits.acquires != 1 || limits.releases != 1 {
		t.Fatalf("timeout status=%d limits=%+v", w.Code, limits)
	}
}

func TestBackgroundClientCancellationStopsUpstream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	upstreamCanceled := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.created\ndata: {}\n\n")
		w.(http.Flusher).Flush()
		cancel()
		<-r.Context().Done()
		close(upstreamCanceled)
	}))
	defer up.Close()
	_, mux, _, limits := backgroundFixture(t, gateway.Target{BaseURL: up.URL})
	r := httptest.NewRequest("GET", "/v1/responses/resp_123?stream=true", nil).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer owner")
	mux.ServeHTTP(httptest.NewRecorder(), r)
	select {
	case <-upstreamCanceled:
	case <-time.After(time.Second):
		t.Fatal("client cancellation did not stop upstream retrieval")
	}
	if limits.releases != 1 {
		t.Fatal("canceled stream lease leaked")
	}
}

func TestBackgroundProxyUsesSavedConfiguration(t *testing.T) {
	var proxied atomic.Int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxied.Add(1)
		if r.URL.Host != "unresolvable.invalid" || r.URL.Path != "/v1/responses/resp_123" || r.Header.Get("Authorization") != "Bearer upstream-secret" {
			t.Errorf("unexpected proxy request %s auth=%q", r.URL, r.Header.Get("Authorization"))
		}
		_, _ = io.WriteString(w, `{"id":"resp_123","status":"completed"}`)
	}))
	defer proxy.Close()
	_, mux, _, limits := backgroundFixture(t, gateway.Target{BaseURL: "http://unresolvable.invalid", ProxyURL: proxy.URL})
	w := backgroundCall(mux, "GET", "/v1/responses/resp_123?proxy_url=https://evil.invalid", "owner")
	if w.Code != 200 || proxied.Load() != 1 || limits.releases != 1 {
		t.Fatalf("proxy status=%d proxied=%d limits=%+v", w.Code, proxied.Load(), limits)
	}
}

func TestBackgroundStreamReadErrorIsReported(t *testing.T) {
	h, _, _, _ := backgroundFixture(t, gateway.Target{BaseURL: "http://unused.invalid"})
	w := httptest.NewRecorder()
	h.copyBackgroundStream(w, backgroundReadError{}, "test")
	if !strings.Contains(w.Body.String(), "response_stream_interrupted") || !w.Flushed {
		t.Fatalf("stream error was hidden: %q", w.Body.String())
	}
}

func TestBackgroundTruncatedSnapshotIsRejected(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = io.WriteString(w, `{"id":"resp_123"}`)
	}))
	defer up.Close()
	_, mux, _, _ := backgroundFixture(t, gateway.Target{BaseURL: up.URL})
	w := backgroundCall(mux, "GET", "/v1/responses/resp_123", "owner")
	if w.Code != 502 || !strings.Contains(w.Body.String(), "response_body_invalid") {
		t.Fatalf("truncated snapshot accepted: status=%d body=%s", w.Code, w.Body.String())
	}
}
