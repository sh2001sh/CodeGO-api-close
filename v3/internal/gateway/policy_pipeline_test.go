package gateway_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/openai"
)

type restrictedAuth struct{ principal gateway.Principal }

func (a restrictedAuth) Authorize(context.Context, string) (gateway.Principal, error) {
	return a.principal, nil
}

func TestCorePolicyRejectsBeforeReserveAndNetwork(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		principal  gateway.Principal
	}{
		{"model", "model_not_allowed", gateway.Principal{AllowedModels: []string{"another-model"}}},
		{"empty model grant", "model_not_allowed", gateway.Principal{AllowedModels: []string{}}},
		{"spoofed IP", "ip_not_allowed", gateway.Principal{AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}},
		{"revoked group", "group_not_allowed", gateway.Principal{Group: "private", AllowedGroups: []string{"default"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var upstreamCalls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				upstreamCalls.Add(1)
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer upstream.Close()
			tc.principal.UserID, tc.principal.KeyID = 7, 70
			settler := newSettler()
			planner := &fakePlanner{targets: []gateway.Target{{Provider: openai.ID, BaseURL: upstream.URL}}}
			g, err := gateway.New(gateway.Deps{Authorizer: restrictedAuth{tc.principal}, Planner: planner,
				Settler: settler, Providers: map[string]gateway.Provider{openai.ID: openai.Provider{}}})
			if err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			g.Register(mux)
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(streamBody))
			req.RemoteAddr = "192.0.2.1:1234"
			req.Header.Set("Authorization", "Bearer sk-test")
			req.Header.Set("X-Forwarded-For", "10.1.2.3")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != 403 || !strings.Contains(w.Body.String(), tc.code) || settler.reserved != 0 || upstreamCalls.Load() != 0 {
				t.Fatalf("status=%d body=%s reserved=%d upstream=%d", w.Code, w.Body.String(), settler.reserved, upstreamCalls.Load())
			}
		})
	}
}
