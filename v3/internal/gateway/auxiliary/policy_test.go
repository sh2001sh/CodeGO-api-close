package auxiliary

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type policyAuth struct{ principal gateway.Principal }

func (a policyAuth) Authorize(context.Context, string) (gateway.Principal, error) {
	return a.principal, nil
}

func TestMandatoryPolicyBeforePlanReserveOrUpstream(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"data":[{"embedding":[1]}],"usage":{"prompt_tokens":1}}`)
	}))
	defer server.Close()
	allowed := netip.MustParsePrefix("10.0.0.0/8")
	trusted := netip.MustParsePrefix("192.0.2.0/24")
	for _, scenario := range []struct {
		name      string
		models    []string
		cidrs     []netip.Prefix
		groups    []string
		peer, xff string
		trusted   []netip.Prefix
		status    int
	}{
		{"model deny", []string{"different"}, nil, nil, "203.0.113.4:1234", "", nil, 403},
		{"empty models deny", []string{}, nil, nil, "203.0.113.4:1234", "", nil, 403},
		{"empty CIDRs deny", nil, []netip.Prefix{}, nil, "10.1.2.3:1234", "", nil, 403},
		{"spoofed forwarded IP denied", nil, []netip.Prefix{allowed}, nil, "203.0.113.4:1234", "10.1.2.3", nil, 403},
		{"untrusted proxy even configured", nil, []netip.Prefix{allowed}, nil, "203.0.113.4:1234", "10.1.2.3", []netip.Prefix{trusted}, 403},
		{"group denied", nil, nil, []string{}, "10.1.2.3:1234", "", nil, 403},
		{"trusted forwarded IP accepted", []string{"alias"}, []netip.Prefix{allowed}, nil, "192.0.2.4:1234", "10.1.2.3", []netip.Prefix{trusted}, 200},
		{"direct accepted", nil, []netip.Prefix{allowed}, nil, "10.1.2.3:1234", "203.0.113.4", nil, 200},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			h, plan, settle, _ := testHandler(t, server.URL)
			h.cfg.Authorizer = policyAuth{gateway.Principal{UserID: 1, Group: "default", AllowedModels: scenario.models, AllowedCIDRs: scenario.cidrs, AllowedGroups: scenario.groups}}
			h.cfg.TrustedProxies = scenario.trusted
			customCalled := false
			h.cfg.ValidateRequest = func(gateway.Principal, string, *http.Request) error { customCalled = true; return nil }
			r := httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(`{"model":"alias","input":"test"}`))
			r.RemoteAddr = scenario.peer
			r.Header.Set("X-Forwarded-For", scenario.xff)
			r.Header.Set("Authorization", "Bearer valid")
			before := calls.Load()
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != scenario.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if scenario.status == 403 && (settle.reserves != 0 || plan.model != "" || calls.Load() != before || customCalled) {
				t.Fatalf("policy bypass reserve=%d planned=%s calls=%d custom=%v", settle.reserves, plan.model, calls.Load()-before, customCalled)
			}
		})
	}
}
