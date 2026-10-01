package baidu

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestOAuthCoalescesConcurrentCallsAndCachesToken(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Query().Get("client_id") != "api+key" || r.URL.Query().Get("client_secret") != "secret&value" || r.URL.Query().Get("grant_type") != "client_credentials" {
			t.Errorf("OAuth credential escaping failed")
		}
		time.Sleep(20 * time.Millisecond)
		_, _ = fmt.Fprint(w, `{"access_token":"fresh-token","expires_in":3600}`)
	}))
	defer server.Close()
	p := Provider{TokenURL: server.URL, Client: server.Client(), Cache: &TokenCache{}}
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			token, err := p.accessToken(context.Background(), "api+key|secret&value")
			if err != nil || token != "fresh-token" {
				t.Errorf("token=%q err=%v", token, err)
			}
		})
	}
	wg.Wait()
	if token, err := p.accessToken(context.Background(), "api+key|secret&value"); err != nil || token != "fresh-token" || calls.Load() != 1 {
		t.Fatalf("cached token=%q err=%v calls=%d", token, err, calls.Load())
	}
}

func TestOAuthExpiredTokenRefreshesAndFailureIsNotCached(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			_, _ = fmt.Fprint(w, `{"error":"invalid_client","error_description":"wrong credential"}`)
			return
		}
		_, _ = fmt.Fprintf(w, `{"access_token":"token-%d","expires_in":1}`, n)
	}))
	defer server.Close()
	p := Provider{TokenURL: server.URL, Client: server.Client(), Cache: &TokenCache{}}
	if _, err := p.accessToken(context.Background(), "key|secret"); err == nil {
		t.Fatal("OAuth error must fail explicitly")
	}
	if token, err := p.accessToken(context.Background(), "key|secret"); err != nil || token != "token-2" {
		t.Fatalf("failed exchange was cached: token=%q err=%v", token, err)
	}
	time.Sleep(950 * time.Millisecond)
	if token, err := p.accessToken(context.Background(), "key|secret"); err != nil || token != "token-3" {
		t.Fatalf("expired token served: token=%q err=%v", token, err)
	}
}

func TestOAuthMalformedResponsesAndCredentials(t *testing.T) {
	for _, fixture := range []string{`{`, `{"expires_in":3600}`, `{"access_token":"t"}`, `{"access_token":"t","expires_in":-1}`} {
		t.Run(fixture, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, fixture) }))
			defer server.Close()
			_, err := (Provider{TokenURL: server.URL, Client: server.Client(), Cache: &TokenCache{}}).accessToken(context.Background(), "key|secret")
			if err == nil {
				t.Fatal("malformed OAuth response accepted")
			}
		})
	}
	for _, secret := range []string{"", "a|", "|b", "a|b|c"} {
		if _, err := (Provider{}).accessToken(context.Background(), secret); err == nil {
			t.Fatal("invalid credential accepted")
		}
	}
}

type failingTransport struct{ err error }

func (r failingTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, r.err }

func TestOAuthCancellationDeadlineAndSecretRedaction(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		p := Provider{Client: &http.Client{Transport: failingTransport{cause}}, Cache: &TokenCache{}}
		_, err := p.BuildRequest(context.Background(), &gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Model: "m", Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}, gateway.Target{Secret: "SENSITIVE_KEY|SENSITIVE_SECRET"})
		if !errors.Is(err, cause) || strings.Contains(err.Error(), "SENSITIVE") {
			t.Fatalf("OAuth lost cancellation or exposed credentials: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Provider{}).accessToken(ctx, "token"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled BuildRequest must not proceed: %v", err)
	}
}
