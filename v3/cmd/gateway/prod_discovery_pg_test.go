//go:build pgintegration

package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProductionModelDiscoveryUsesWarmIdentityAndCatalog(t *testing.T) {
	f := newContractFixture(t)
	warmNativeFixture(t, f)
	before, calls := f.balances(t), f.upstreamCalls.Load()
	for _, scenario := range []struct {
		path, address string
		authorized    bool
		status        int
	}{
		{"/v1/models", "127.0.0.1:3456", true, http.StatusOK},
		{"/v1/models/contract-model", "127.0.0.1:3456", true, http.StatusOK},
		{"/v1beta/models", "127.0.0.1:3456", true, http.StatusOK},
		{"/v1beta/openai/models", "127.0.0.1:3456", true, http.StatusOK},
		{"/v1/models/hidden-model", "127.0.0.1:3456", true, http.StatusNotFound},
		{"/v1/models", "127.0.0.1:3456", false, http.StatusUnauthorized},
		{"/v1/models", "198.51.100.7:3456", true, http.StatusForbidden},
	} {
		r := httptest.NewRequest(http.MethodGet, scenario.path, nil)
		r.RemoteAddr = scenario.address
		r.Header.Set("X-Forwarded-For", "127.0.0.1")
		if scenario.authorized {
			r.Header.Set("Authorization", "Bearer "+f.key)
		}
		f.counts.start()
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		counts := f.counts.stop()
		if w.Code != scenario.status {
			t.Fatalf("model discovery %s status=%d body=%s", scenario.path, w.Code, w.Body)
		}
		if scenario.status == http.StatusOK && !strings.Contains(w.Body.String(), "contract-model") {
			t.Fatalf("model discovery %s missing usable model: %s", scenario.path, w.Body)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("authorized discovery response can be cached across keys")
		}
		if len(counts.postgres) != 0 || len(counts.redis) != 0 {
			t.Fatalf("warm discovery %s touched PG=%v Redis=%v", scenario.path, counts.postgres, counts.redis)
		}
	}
	if f.upstreamCalls.Load() != calls {
		t.Fatal("model metadata reached upstream")
	}
	assertContractDebit(t, before, f.balances(t), 0)
	f.assertNoHolds(t)
}
