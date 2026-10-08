package gateway_test

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/openai"
	"github.com/sh2001sh/new-api/v3/internal/gateway/routing"
)

func TestRequestGroupSelectsModelsWithoutChangingKeyDefault(t *testing.T) {
	f := newDiscoveryFixture(t)
	f.auth.principal.AllowedGroups = []string{"default", "private"}
	f.auth.principal.CrossGroupRetry = true
	headers := map[string]string{"Authorization": "Bearer sk-test", "X-CodeGo-Group": "private"}
	if got := discoveryIDs(t, f.get("/v1/models", headers)); !reflect.DeepEqual(got, []string{"hidden"}) {
		t.Fatalf("selected group's models = %v", got)
	}
	if f.auth.principal.Group != "default" || !f.auth.principal.CrossGroupRetry {
		t.Fatal("request mutated authenticated key defaults")
	}
	// No header retains the key's normal cross-group discovery behavior.
	if got := discoveryIDs(t, f.get("/v1/models", discoveryBearer)); !reflect.DeepEqual(got, []string{"alpha", "hidden", "zeta"}) {
		t.Fatalf("default discovery changed = %v", got)
	}
}

func TestRequestGroupKeepsSelectedRouteAndPricingForSettlement(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer private-secret" {
			t.Errorf("wrong upstream route credential: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"test","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"selected private route"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer upstream.Close()
	f := newDiscoveryFixture(t)
	f.auth.principal.AllowedGroups = []string{"default", "private"}
	f.auth.principal.CrossGroupRetry = true
	snapshot := f.current.Load()
	snapshot.Groups["private"] = catalog.Group{Name: "private", Multiplier: 1.75}
	snapshot.Channels[1].BaseURL = upstream.URL
	snapshot.Channels[1].Credentials = []catalog.Credential{{ID: 11, Secret: "default-secret"}}
	snapshot.Channels[2].Provider, snapshot.Channels[2].BaseURL = openai.ID, upstream.URL
	snapshot.Channels[2].Credentials = []catalog.Credential{{ID: 22, Secret: "private-secret"}}
	snapshot.Routes["private"]["alpha"] = []catalog.Route{{ChannelID: 2}}
	settler := newSettler()
	g, err := gateway.New(gateway.Deps{Authorizer: f.auth, Planner: routing.New(f.current.Load, routing.Config{}),
		Settler: settler, Providers: map[string]gateway.Provider{openai.ID: openai.Provider{}}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	g.Register(mux)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"alpha","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer sk-test")
	req.Header.Set("X-CodeGo-Group", "private")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "selected private route") || calls.Load() != 1 {
		t.Fatalf("status=%d body=%s calls=%d", w.Code, w.Body.String(), calls.Load())
	}
	outcome := <-settler.outcomes
	if outcome.Target == nil || outcome.Target.ChannelID != 2 || outcome.Target.Group != "private" || outcome.Target.MultiplierPPM != 1750000 {
		t.Fatalf("wrong settlement target: %+v", outcome.Target)
	}
	if f.auth.principal.Group != "default" {
		t.Fatal("key default was mutated")
	}
}

func TestExplicitGroupDoesNotFallBackToAnotherGroup(t *testing.T) {
	f := newDiscoveryFixture(t)
	f.auth.principal.AllowedGroups = []string{"default", "private"}
	f.auth.principal.CrossGroupRetry = true
	settler := newSettler()
	g, err := gateway.New(gateway.Deps{Authorizer: f.auth, Planner: routing.New(f.current.Load, routing.Config{}), Settler: settler, Providers: map[string]gateway.Provider{openai.ID: openai.Provider{}}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	g.Register(mux)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"alpha","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer sk-test")
	req.Header.Set("X-CodeGo-Group", "private")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "no_available_channel") || settler.reserved != 0 {
		t.Fatalf("explicit group silently fell back: status=%d body=%s reserved=%d", w.Code, w.Body.String(), settler.reserved)
	}
}

func TestRequestGroupCannotBypassLatestSnapshotGrants(t *testing.T) {
	f := newDiscoveryFixture(t)
	f.auth.principal.AllowedGroups = []string{"default", "private"}
	f.current.Load().AccountProfiles = map[int64]catalog.AccountProfile{7: {AllowedGroups: []string{"default"}}}
	if got := discoveryIDs(t, f.get("/v1/models", map[string]string{"Authorization": "Bearer sk-test", "X-CodeGo-Group": "private"})); len(got) != 0 {
		t.Fatalf("stale key grant exposed revoked group: %v", got)
	}
}

func TestRequestGroupDenialsPrecedeBillingAndNetwork(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values []string
		status int
		code   string
	}{
		{"unauthorized private group", []string{"private"}, 403, "group_not_allowed"},
		{"virtual card without entitlement", []string{"zero-hour"}, 403, "group_not_allowed"},
		{"multiple values", []string{"default", "private"}, 400, "invalid_group"},
		{"comma separated", []string{"default,private"}, 400, "invalid_group"},
		{"empty", []string{""}, 400, "invalid_group"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newDiscoveryFixture(t)
			settler := newSettler()
			g, err := gateway.New(gateway.Deps{Authorizer: f.auth, Planner: routing.New(f.current.Load, routing.Config{}), Settler: settler, Providers: map[string]gateway.Provider{openai.ID: openai.Provider{}}})
			if err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			g.Register(mux)
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(streamBody))
			req.Header.Set("Authorization", "Bearer sk-test")
			for _, value := range tc.values {
				req.Header.Add("X-CodeGo-Group", value)
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) || settler.reserved != 0 {
				t.Fatalf("status=%d body=%s reserved=%d", w.Code, w.Body.String(), settler.reserved)
			}
			read := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			read.Header = req.Header.Clone()
			result := httptest.NewRecorder()
			f.mux.ServeHTTP(result, read)
			if result.Code != tc.status || !strings.Contains(result.Body.String(), tc.code) {
				t.Fatalf("model discovery status=%d body=%s", result.Code, result.Body.String())
			}
		})
	}
}
