package gateway_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/routing"
)

type discoveryAuth struct {
	principal gateway.Principal
	err       error
}

func (a *discoveryAuth) Authorize(_ context.Context, key string) (gateway.Principal, error) {
	if key != "sk-test" {
		return gateway.Principal{}, gateway.ErrInvalidKey
	}
	return a.principal, a.err
}

type discoveryFixture struct {
	mux     *http.ServeMux
	auth    *discoveryAuth
	current atomic.Pointer[catalog.Snapshot]
}

func newDiscoveryFixture(t *testing.T) *discoveryFixture {
	t.Helper()
	f := &discoveryFixture{mux: http.NewServeMux(), auth: &discoveryAuth{principal: gateway.Principal{
		UserID: 7, KeyID: 70, Group: "default", AllowedGroups: []string{"default"},
	}}}
	f.current.Store(&catalog.Snapshot{
		Groups: map[string]catalog.Group{"default": {Name: "default", Multiplier: 1}, "private": {Name: "private", Multiplier: 1}},
		Channels: map[int64]*catalog.Channel{
			1: {ID: 1, Provider: "openai", Credentials: []catalog.Credential{{ID: 11}}},
			2: {ID: 2, Provider: "anthropic", Credentials: []catalog.Credential{{ID: 22}}},
		},
		Routes: map[string]map[string][]catalog.Route{
			"default": {"alpha": {{ChannelID: 1}}, "zeta": {{ChannelID: 1}}, "unpriced": {{ChannelID: 1}}},
			"private": {"hidden": {{ChannelID: 2}}},
		},
		Prices: map[string]catalog.Price{"alpha": {Mode: "per_request", PerRequest: 1}, "zeta": {Mode: "per_request", PerRequest: 1}, "hidden": {Mode: "per_request", PerRequest: 1}},
	})
	planner := routing.New(f.current.Load, routing.Config{})
	discovery, err := gateway.NewModelDiscovery(gateway.ModelDiscoveryConfig{
		Authorizer: f.auth, Planner: planner, Snapshot: f.current.Load,
	})
	if err != nil {
		t.Fatal(err)
	}
	discovery.Register(f.mux)
	return f
}

func (f *discoveryFixture) get(path string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.RemoteAddr = "203.0.113.7:4040"
	for key, value := range headers {
		r.Header.Set(key, value)
	}
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)
	return w
}

var discoveryBearer = map[string]string{"Authorization": "Bearer sk-test"}

func discoveryIDs(t *testing.T, w *httptest.ResponseRecorder) []string {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Object string `json:"object"`
		Data   []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Object != "list" || body.Data == nil {
		t.Fatalf("invalid list shape: %s", w.Body.String())
	}
	ids := make([]string, 0, len(body.Data))
	for _, item := range body.Data {
		ids = append(ids, item.ID)
	}
	return ids
}

func TestModelDiscoveryOnlyListsCurrentlyUsableModels(t *testing.T) {
	f := newDiscoveryFixture(t)
	w := f.get("/v1/models", discoveryBearer)
	if ids := discoveryIDs(t, w); !reflect.DeepEqual(ids, []string{"alpha", "zeta"}) {
		t.Fatalf("visible models = %v", ids)
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Request-Id") == "" {
		t.Fatalf("missing private response headers: %v", w.Header())
	}
	for _, name := range []string{"hidden", "unknown", "unpriced"} {
		if w := f.get("/v1/models/"+name, discoveryBearer); w.Code != http.StatusNotFound {
			t.Fatalf("%s detail status=%d body=%s", name, w.Code, w.Body.String())
		}
	}
	if w := f.get("/v1/models/alpha", discoveryBearer); w.Code != http.StatusOK {
		t.Fatalf("known detail: %d %s", w.Code, w.Body.String())
	}
}

func TestModelDiscoveryHonorsNilAndEmptyModelACL(t *testing.T) {
	f := newDiscoveryFixture(t)
	f.auth.principal.AllowedModels = []string{}
	if ids := discoveryIDs(t, f.get("/v1/models", discoveryBearer)); len(ids) != 0 {
		t.Fatalf("empty ACL leaked %v", ids)
	}
	if w := f.get("/v1/models/alpha", discoveryBearer); w.Code != http.StatusNotFound {
		t.Fatalf("disallowed detail status=%d", w.Code)
	}
	f.auth.principal.AllowedModels = []string{"alpha", "hidden"}
	if ids := discoveryIDs(t, f.get("/v1/models", discoveryBearer)); !reflect.DeepEqual(ids, []string{"alpha"}) {
		t.Fatalf("ACL bypassed route permission: %v", ids)
	}
	f.auth.principal.AllowedModels = nil
	if ids := discoveryIDs(t, f.get("/v1/models", discoveryBearer)); len(ids) != 2 {
		t.Fatalf("nil ACL hid models: %v", ids)
	}
}

func TestModelDiscoveryRechecksRotatedCatalogPolicies(t *testing.T) {
	f := newDiscoveryFixture(t)
	_ = discoveryIDs(t, f.get("/v1/models", discoveryBearer)) // warm route indexes
	old := f.current.Load()
	rotated := *old
	rotated.Channels = map[int64]*catalog.Channel{2: old.Channels[2]}
	f.current.Store(&rotated)
	if ids := discoveryIDs(t, f.get("/v1/models", discoveryBearer)); len(ids) != 0 {
		t.Fatalf("disabled channel remained visible: %v", ids)
	}
	rotated = *old
	rotated.AccountProfiles = map[int64]catalog.AccountProfile{7: {AllowedGroups: []string{}}}
	f.current.Store(&rotated)
	if ids := discoveryIDs(t, f.get("/v1/models", discoveryBearer)); len(ids) != 0 {
		t.Fatalf("revoked group remained visible: %v", ids)
	}
	rotated = *old
	channel := *old.Channels[1]
	channel.Credentials = []catalog.Credential{{ID: 11, ExpiresAt: time.Now().Add(-time.Hour)}}
	rotated.Channels = map[int64]*catalog.Channel{1: &channel}
	f.current.Store(&rotated)
	if w := f.get("/v1/models/alpha", discoveryBearer); w.Code != http.StatusNotFound {
		t.Fatalf("expired credential remained visible: %d %s", w.Code, w.Body.String())
	}
}

func TestModelDiscoveryAuthenticatesBeforeReadingCatalog(t *testing.T) {
	f := newDiscoveryFixture(t)
	f.current.Store(nil)
	for _, headers := range []map[string]string{nil, {"Authorization": "Bearer invalid"}} {
		if w := f.get("/v1/models", headers); w.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated status=%d body=%s", w.Code, w.Body.String())
		}
	}
	f.auth.err = gateway.ErrAuthUnavailable
	if w := f.get("/v1/models", discoveryBearer); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("auth outage status=%d body=%s", w.Code, w.Body.String())
	}
	f.auth.err = nil
	if w := f.get("/v1/models", discoveryBearer); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("catalog outage status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestModelDiscoveryHonorsClientAddressAndRevokedGroups(t *testing.T) {
	f := newDiscoveryFixture(t)
	for _, cidrs := range [][]netip.Prefix{{}, {netip.MustParsePrefix("198.51.100.0/24")}} {
		f.auth.principal.AllowedCIDRs = cidrs
		if w := f.get("/v1/models", discoveryBearer); w.Code != http.StatusForbidden {
			t.Fatalf("disallowed network status=%d body=%s", w.Code, w.Body.String())
		}
	}
	f.auth.principal.AllowedCIDRs = []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}
	if ids := discoveryIDs(t, f.get("/v1/models", discoveryBearer)); len(ids) != 2 {
		t.Fatalf("allowed network models=%v", ids)
	}
	f.auth.principal.AllowedGroups = []string{}
	if w := f.get("/v1/models", discoveryBearer); w.Code != http.StatusForbidden {
		t.Fatalf("revoked token group status=%d body=%s", w.Code, w.Body.String())
	}
}
