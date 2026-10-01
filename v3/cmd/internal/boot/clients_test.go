package boot

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/credentials"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/httpx"
)

func TestTargetClientsApplyMarketNetworkPolicyWithoutChangingOfficialChannels(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	identities := credentials.NewTransportPool(credentials.TransportConfig{})
	defer identities.CloseIdle()
	transports := httpx.NewPool(httpx.TransportConfig{})
	defer transports.CloseIdle()
	clients := TargetClients(identities, transports)
	market, err := clients(context.Background(), gateway.Target{Scope: "marketplace"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = market.Get(upstream.URL); !errors.Is(err, httpx.ErrMarketTransportPolicy) || calls.Load() != 0 {
		t.Fatalf("market private upstream reached: calls=%d err=%v", calls.Load(), err)
	}
	official, err := clients(context.Background(), gateway.Target{Scope: "official"})
	if err != nil {
		t.Fatal(err)
	}
	response, err := official.Get(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err = response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusNoContent || calls.Load() != 1 {
		t.Fatalf("official upstream changed: status=%d calls=%d", response.StatusCode, calls.Load())
	}
	if _, err = clients(context.Background(), gateway.Target{Scope: "marketplace", ProxyURL: "://invalid"}); err == nil {
		t.Fatal("invalid configured proxy silently accepted")
	}
}
