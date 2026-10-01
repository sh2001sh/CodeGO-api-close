package workflow_test

import (
	"context"
	"net/http"
	"net/netip"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

type policyAuth struct{ principal gateway.Principal }

func (a policyAuth) Authorize(context.Context, string) (gateway.Principal, error) {
	return a.principal, nil
}

func TestWorkflowPolicyCannotBeBypassedByOptionalValidator(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*gateway.Principal)
	}{
		{"model", func(p *gateway.Principal) { p.AllowedModels = []string{"another-video"} }},
		{"empty_models", func(p *gateway.Principal) { p.AllowedModels = []string{} }},
		{"address", func(p *gateway.Principal) { p.AllowedCIDRs = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")} }},
		{"empty_addresses", func(p *gateway.Principal) { p.AllowedCIDRs = []netip.Prefix{} }},
		{"group", func(p *gateway.Principal) { p.AllowedGroups = []string{"vip"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			principal := gateway.Principal{UserID: 11, KeyID: 111, Group: "default"}
			tc.edit(&principal)
			f := newAPIFixture(t, func(c *workflow.Config) {
				c.Authorizer = policyAuth{principal}
				c.ValidateRequest = func(gateway.Principal, string, *http.Request) error { return nil }
			})
			w := f.request("POST", "/v1/videos", "owner", `{"model":"video"}`, "")
			if w.Code != 403 || f.provider.submits.Load() != 0 || len(f.settler.reserved) != 0 {
				t.Fatalf("policy bypassed: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestWorkflowPolicyChecksModelForOwnedTaskReads(t *testing.T) {
	for _, route := range []struct{ method, path, body string }{
		{"GET", "/v1/videos/owned", ""}, {"GET", "/v1/videos/owned/content", ""},
		{"POST", "/suno/fetch", `{"ids":["owned"]}`},
	} {
		t.Run(route.path, func(t *testing.T) {
			f := newAPIFixture(t, func(c *workflow.Config) {
				c.Authorizer = policyAuth{gateway.Principal{UserID: 11, KeyID: 111, Group: "default", AllowedModels: []string{"another"}}}
			})
			provider := "openai_video"
			if route.path == "/suno/fetch" {
				provider = "suno"
			}
			f.repo.seed(ownedTask("owned", provider))
			w := f.request(route.method, route.path, "owner", route.body, "")
			if w.Code != 403 || f.provider.polls.Load()+f.provider.contents.Load() != 0 {
				t.Fatalf("restricted task read: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestSunoUppercaseLyricsUsesLyricsBillingModel(t *testing.T) {
	f := newAPIFixture(t, func(c *workflow.Config) {
		c.Planner = apiPlanner{gateway.Target{ChannelID: 4, CredentialID: 9, Provider: "suno", UpstreamModel: "suno_lyrics"}}
	})
	f.provider.submitFn = func(_ context.Context, _ gateway.Target, in native.Submit) (native.Result, error) {
		if in.Model != "suno_lyrics" {
			t.Fatalf("lyrics routed under model %s", in.Model)
		}
		return native.Result{ID: "lyrics-native", Status: "queued"}, nil
	}
	w := f.request("POST", "/suno/submit/LYRICS", "owner", `{"prompt":"rain"}`, "")
	if w.Code != 200 || f.repo.one(t).Model != "suno_lyrics" {
		t.Fatalf("lyrics billing: %d %s", w.Code, w.Body.String())
	}
}
