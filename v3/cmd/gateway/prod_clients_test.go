package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sh2001sh/new-api/v3/cmd/internal/boot"
	"github.com/sh2001sh/new-api/v3/internal/credentials"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/httpx"
)

func TestProductionClientUsesCredentialIdentityAndRejectsBadConfig(t *testing.T) {
	identities := credentials.NewTransportPool(credentials.TransportConfig{})
	transports := httpx.NewPool(httpx.TransportConfig{})
	t.Cleanup(identities.CloseIdle)
	t.Cleanup(transports.CloseIdle)
	resolve := boot.TargetClients(identities, transports)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.UserAgent() != "credential-specific-browser" {
			t.Errorf("upstream UA=%q", r.UserAgent())
		}
		w.WriteHeader(204)
	}))
	t.Cleanup(upstream.Close)
	client, err := resolve(context.Background(), gateway.Target{CredentialID: 8,
		Fingerprint: gateway.CredentialFingerprint{UserAgent: "credential-specific-browser", TLSProfile: "firefox"}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Get(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err = response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 204 {
		t.Fatalf("upstream status=%d", response.StatusCode)
	}
	for _, target := range []gateway.Target{
		{CredentialID: 9, Fingerprint: gateway.CredentialFingerprint{TLSProfile: "unknown"}},
		{CredentialID: 9, Fingerprint: gateway.CredentialFingerprint{UserAgent: "bad\nagent"}},
		{CredentialID: 9, ProxyURL: "ftp://unsupported-proxy"},
	} {
		if _, err = resolve(context.Background(), target); err == nil {
			t.Fatal("invalid credential/proxy configuration silently accepted")
		}
	}
}
