package auxiliary

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestBaiduVectorOAuthCacheAndConcurrentBuild(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/oauth/token" || r.URL.Query().Get("grant_type") != "client_credentials" || r.URL.Query().Get("client_id") != "api-id" || r.URL.Query().Get("client_secret") != "signing-secret" {
			t.Errorf("incorrect OAuth wire request")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"fresh-token","expires_in":3600}`)
	}))
	defer server.Close()
	adapter := &vectorBaiduAdapter{Client: server.Client(), TokenURL: server.URL + "/oauth/token"}
	var workers sync.WaitGroup
	for range 10 {
		workers.Go(func() {
			wire, err := adapter.Build(context.Background(), &gateway.Request{Model: "Embedding-V1"}, gateway.Target{BaseURL: server.URL, Secret: "api-id|signing-secret"}, Input{Operation: Embeddings, Body: []byte(`{"input":["one","two"]}`)})
			if err != nil {
				t.Error(err)
				return
			}
			defer func() { _ = wire.Body.Close() }()
			if wire.URL.Path != "/rpc/2.0/ai_custom/v1/wenxinworkshop/embeddings/embedding-v1" || wire.URL.Query().Get("access_token") != "fresh-token" {
				t.Error("incorrect embedding OAuth request")
			}
			if wire.Header.Get("Authorization") != "" {
				t.Error("signing secret leaked to embedding bearer header")
			}
		})
	}
	workers.Wait()
	if calls.Load() != 1 {
		t.Fatalf("concurrent OAuth exchanges = %d, want 1", calls.Load())
	}
	adapter.mu.Lock()
	adapter.tokens[adapter.TokenURL+"\x00api-id|signing-secret"] = vectorBaiduToken{value: "expired", expires: time.Now().Add(-time.Second)}
	adapter.mu.Unlock()
	value, err := adapter.accessToken(context.Background(), "api-id|signing-secret")
	if err != nil || value != "fresh-token" || calls.Load() != 2 {
		t.Fatalf("expired refresh: value=%s calls=%d error=%v", value, calls.Load(), err)
	}
}

func TestBaiduVectorOAuthFailures(t *testing.T) {
	for _, body := range []string{`{"error":"invalid_client"}`, `{"access_token":"token","expires_in":0}`, `{"access_token":"token","expires_in":99999999999}`, `{"expires_in":100}`, `[]`, `{`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) }))
		adapter := &vectorBaiduAdapter{Client: server.Client(), TokenURL: server.URL}
		_, err := adapter.accessToken(context.Background(), "api-id|signing-secret")
		if err == nil {
			t.Errorf("accepted OAuth response %s", body)
		}
		if err != nil && (strings.Contains(err.Error(), "api-id") || strings.Contains(err.Error(), "signing-secret")) {
			t.Error("OAuth error contains credential")
		}
		server.Close()
	}
	for _, secret := range []string{"", "|secret", "api|", "api|secret|extra"} {
		_, err := (&vectorBaiduAdapter{}).accessToken(context.Background(), secret)
		if err == nil {
			t.Error("accepted invalid OAuth credentials")
		}
	}
}

func TestBaiduVectorOAuthCancellationAndTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	adapter := &vectorBaiduAdapter{Client: server.Client(), TokenURL: server.URL}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := adapter.accessToken(ctx, "api-id|signing-secret")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled OAuth = %v", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	_, err = adapter.accessToken(ctx, "api-id|signing-secret")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("OAuth timeout = %v", err)
	}
	if strings.Contains(err.Error(), "signing-secret") || strings.Contains(err.Error(), "client_secret") {
		t.Error("OAuth network error leaked credential URL")
	}
}

func TestBaiduVectorNativeModelPaths(t *testing.T) {
	for model, path := range map[string]string{"Embedding-V1": "embedding-v1", "bge-large-en": "bge_large_en", "bge-large-zh": "bge_large_zh", "tao-8k": "tao_8k"} {
		wire, err := (&vectorBaiduAdapter{}).Build(context.Background(), &gateway.Request{Model: model}, gateway.Target{BaseURL: "https://api.example", Secret: "token"}, Input{Operation: Embeddings, Body: []byte(`{"input":"one"}`)})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(wire.URL.Path, "/embeddings/"+path) {
			t.Errorf("%s => %s", model, wire.URL.Path)
		}
		_ = wire.Body.Close()
	}
	_, err := (&vectorBaiduAdapter{}).Build(context.Background(), &gateway.Request{Model: "evil/../../model"}, gateway.Target{BaseURL: "https://api.example", Secret: "token"}, Input{Operation: Embeddings, Body: []byte(`{"input":"one"}`)})
	if err == nil {
		t.Error("accepted path traversal model")
	}
}

type vectorRoundTripFunc func(*http.Request) (*http.Response, error)

func (f vectorRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBaiduVectorOAuthUsesChannelTransport(t *testing.T) {
	called := false
	client := &http.Client{Transport: vectorRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		called = true
		if r.URL.Host != "oauth.example" {
			t.Errorf("OAuth host = %s", r.URL.Host)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"access_token":"token","expires_in":3600}`)), Header: make(http.Header)}, nil
	}), CheckRedirect: noRedirect}
	ctx := context.WithValue(context.Background(), clientKey{}, client)
	value, err := (&vectorBaiduAdapter{TokenURL: "https://oauth.example/token"}).accessToken(ctx, "id|secret")
	if err != nil || value != "token" || !called {
		t.Fatalf("channel transport not used: token=%s called=%v err=%v", value, called, err)
	}
}
