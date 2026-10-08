package boot

import (
	"context"
	"net/http"

	"github.com/sh2001sh/new-api/v3/internal/credentials"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/httpx"
)

// TargetClients keeps the same credential identity for gateway requests and
// worker recovery. A configured proxy or fingerprint failure is returned.
func TargetClients(identity *credentials.TransportPool, transports *httpx.Pool) gateway.ClientProvider {
	return func(_ context.Context, target gateway.Target) (*http.Client, error) {
		var client *http.Client
		var err error
		if target.Fingerprint.UserAgent != "" || target.Fingerprint.TLSProfile != "" || target.Provider == "codex" {
			client, _, err = identity.Client(target.CredentialID, target.ProxyURL, credentials.Fingerprint{
				UserAgent: target.Fingerprint.UserAgent, TLSProfile: target.Fingerprint.TLSProfile,
			})
		} else {
			var transport http.RoundTripper
			transport, err = transports.Transport(target.ProxyURL)
			client = &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			}}
		}
		if err != nil {
			return nil, err
		}
		if target.Scope == "marketplace" {
			return transports.MarketClient(target.CredentialID, client)
		}
		return client, nil
	}
}
