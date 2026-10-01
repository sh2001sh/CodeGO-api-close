package gateway_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/credentials"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/bridge"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/openai"
)

func clientIdentityHarness(t *testing.T, target gateway.Target, adapter gateway.Provider, clients gateway.ClientProvider) *harness {
	t.Helper()
	planner := &fakePlanner{targets: []gateway.Target{target}}
	settler := newSettler()
	g, err := gateway.New(gateway.Deps{Authorizer: fakeAuth{}, Planner: planner, Settler: settler,
		Providers: map[string]gateway.Provider{target.Provider: adapter}, Clients: clients})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	g.Register(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return &harness{t: t, gw: server, planner: planner, settler: settler}
}

func TestConfiguredCredentialClientActuallyCarriesIdentity(t *testing.T) {
	seenUA := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenUA <- r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	t.Cleanup(upstream.Close)
	pool := credentials.NewTransportPool(credentials.TransportConfig{})
	t.Cleanup(pool.CloseIdle)
	target := gateway.Target{Provider: openai.ID, CredentialID: 25, BaseURL: upstream.URL,
		Fingerprint: gateway.CredentialFingerprint{UserAgent: "fixture-credential-agent", TLSProfile: "firefox"}}
	clients := func(_ context.Context, selected gateway.Target) (*http.Client, error) {
		if selected.CredentialID != 25 {
			t.Fatalf("credential identity was lost: %d", selected.CredentialID)
		}
		client, _, err := pool.Client(selected.CredentialID, selected.ProxyURL, credentials.Fingerprint{
			UserAgent: selected.Fingerprint.UserAgent, TLSProfile: selected.Fingerprint.TLSProfile})
		return client, err
	}
	h := clientIdentityHarness(t, target, bridge.Provider{Chat: openai.Provider{}}, clients)
	view := h.do(streamBody)
	_ = h.outcome()
	if view.status != 200 || <-seenUA != "fixture-credential-agent" {
		t.Fatalf("configured upstream identity unused: %+v", view)
	}
}

func TestNativeTransportDoesNotMutateSharedCredentialClient(t *testing.T) {
	native := &nativeTransport{}
	fallback := &http.Transport{}
	shared := &http.Client{Transport: fallback}
	clients := func(context.Context, gateway.Target) (*http.Client, error) { return shared, nil }
	h := clientIdentityHarness(t, gateway.Target{Provider: "native", BaseURL: "http://127.0.0.1:1"}, bridge.Provider{Chat: native}, clients)
	view := h.do(streamBody)
	_ = h.outcome()
	if view.status != 200 || native.calls.Load() != 1 || shared.Transport != fallback || !strings.Contains(strings.Join(view.data, ""), "native") {
		t.Fatalf("native dispatch or shared client isolation failed: %+v", view)
	}
}
