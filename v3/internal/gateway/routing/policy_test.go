package routing

import (
	"errors"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

func marketFixture() *catalog.Snapshot {
	s := snapshotOf("fill_first", chanSpec{id: 1, creds: 1, weight: 1})
	s.Channels[1].Scope, s.Channels[1].OwnerUserID = "marketplace", 7
	s.Market = catalog.MarketSnapshot{
		Groups:   map[string]catalog.MarketGroupPolicy{"market": {Status: "active", Visibility: "private", OwnerUserID: 7, Allowed: map[int64]bool{1: true}}},
		Channels: map[int64]catalog.MarketChannelPolicy{1: {GroupName: "market", MultiplierPPM: 1500000, Blocked: map[int64]bool{}, UserMultipliers: map[int64]int64{}}},
	}
	return s
}

func TestMarketPermissionsApplyToEveryRoutingPath(t *testing.T) {
	for _, path := range []string{"weighted", "affinity", "all_cooling"} {
		for _, refusal := range []string{"access", "blocked", "paused", "key_cap", "pool_owner", "pool_cap", "missing_policy", "expiry"} {
			t.Run(path+"/"+refusal, func(t *testing.T) {
				s := marketFixture()
				if path == "weighted" {
					s.Routes["default"]["gpt"][0].Strategy = "weighted"
				}
				e := newEnv(s, Config{})
				r := request(`{"model":"gpt","user":"session"}`)
				first := mustPlan(t, e, r)[0]
				if path == "all_cooling" {
					e.planner.Report(first, credFail)
				}
				policy := s.Market.Channels[1]
				group := s.Market.Groups["market"]
				switch refusal {
				case "access":
					delete(group.Allowed, 1)
				case "blocked":
					policy.Blocked[1] = true
				case "paused":
					group.Status = "paused"
				case "key_cap":
					r.Principal.MaxMarketplaceMultiplierPPM = 1000000
				case "pool_owner":
					s.Market.Pools = map[string]catalog.MarketPoolPolicy{"default": {OwnerUserID: 2}}
				case "pool_cap":
					s.Market.Pools = map[string]catalog.MarketPoolPolicy{"default": {OwnerUserID: 1, MaxMultiplierPPM: 1000000}}
				case "missing_policy":
					delete(s.Market.Channels, 1)
				case "expiry":
					s.Channels[1].Credentials[0].ExpiresAt = e.clock.now()
				}
				s.Market.Groups["market"] = group
				if refusal != "missing_policy" {
					s.Market.Channels[1] = policy
				}
				if targets, err := e.planner.Plan(ctx, r); !errors.Is(err, ErrNoRoute) || len(targets) != 0 {
					t.Fatalf("denied route produced %d targets and err=%v", len(targets), err)
				}
			})
		}
	}
}

func TestMarketMultiplierWindowExpiresLocally(t *testing.T) {
	s := marketFixture()
	e := newEnv(s, Config{})
	policy := s.Market.Channels[1]
	policy.Windows = []catalog.MarketMultiplierWindow{{StartsAt: e.clock.now().Add(-time.Hour), EndsAt: e.clock.now().Add(time.Second), MultiplierPPM: 500000}}
	s.Market.Channels[1] = policy
	r := request("")
	r.Principal.MaxMarketplaceMultiplierPPM = 1000000
	if target := mustPlan(t, e, r)[0]; target.Group != "market" || target.MultiplierPPM != 500000 || target.OwnerUserID != 7 {
		t.Fatalf("market facts missing: group=%s multiplier=%d owner=%d", target.Group, target.MultiplierPPM, target.OwnerUserID)
	}
	e.clock.advance(time.Second)
	if _, err := e.planner.Plan(ctx, r); !errors.Is(err, ErrNoRoute) {
		t.Fatalf("expired cheaper window: %v", err)
	}
}

func TestTargetRetainsSavedConfiguration(t *testing.T) {
	s := snapshotOf("fill_first", chanSpec{id: 1, creds: 1, weight: 1})
	c := s.Channels[1]
	c.Settings, c.ParamOverride = map[string]any{"reasoning": "high"}, map[string]any{"temperature": 0}
	c.HeaderOverride, c.StatusCodeMapping = map[string]string{"X-Version": "one"}, map[string]int{"400": 503}
	c.Credentials[0].Fingerprint = catalog.CredentialFingerprint{UserAgent: "stable-ua", TLSProfile: "chrome"}
	got := mustPlan(t, newEnv(s, Config{}), request(""))[0]
	if got.Settings["reasoning"] != "high" || got.ParamOverride["temperature"] != 0 || got.HeaderOverride["X-Version"] != "one" || got.StatusCodeMapping["400"] != 503 || got.Fingerprint.UserAgent != "stable-ua" || got.Fingerprint.TLSProfile != "chrome" {
		t.Fatal("target lost persisted channel configuration")
	}
}

func TestAutoGroupsAndCrossGroupRetryRemainFrozen(t *testing.T) {
	s := snapshotOf("fill_first", chanSpec{id: 1, creds: 1, weight: 1}, chanSpec{id: 2, creds: 1, weight: 1})
	s.Routes["default"]["gpt"] = s.Routes["default"]["gpt"][:1]
	s.Routes["second"] = map[string][]catalog.Route{"gpt": {{ChannelID: 2, Weight: 1, Strategy: "fill_first"}}}
	s.Groups = map[string]catalog.Group{"default": {Multiplier: 1}, "second": {Multiplier: 1.5}}
	e := newEnv(s, Config{})
	r := request("")
	r.Principal.AutoGroups = []string{"default", "second"}
	if got := mustPlan(t, e, r); len(got) != 1 {
		t.Fatalf("cross-group retries were enabled implicitly: %d", len(got))
	}
	r.Principal.CrossGroupRetry = true
	plan := mustPlan(t, e, r)
	if len(plan) != 2 || plan[0].Group != "default" || plan[1].Group != "second" || plan[1].MultiplierPPM != 1500000 {
		t.Fatal("cross-group plan/pricing metadata lost")
	}
	r.Principal.Group = "auto"
	if got := mustPlan(t, e, r); len(got) != 2 {
		t.Fatalf("auto groups: %d", len(got))
	}
	e.clock.advance(time.Second)
	if plan[1].MultiplierPPM != 1500000 {
		t.Fatal("existing plan changed after clock advance")
	}
}

func TestOfficialGroupsCheckLatestAuthorization(t *testing.T) {
	s := snapshotOf("fill_first", chanSpec{id: 1, creds: 1, weight: 1})
	r := request("")
	r.Principal.AllowedGroups = []string{"default"}
	s.AccountProfiles = map[int64]catalog.AccountProfile{1: {AllowedGroups: []string{"another"}}}
	e := newEnv(s, Config{})
	if _, err := e.planner.Plan(ctx, r); !errors.Is(err, ErrNoRoute) {
		t.Fatalf("stale key grant allowed official route: %v", err)
	}
	s.AccountProfiles[1] = catalog.AccountProfile{AllowedGroups: []string{"default"}}
	if got := mustPlan(t, e, r); len(got) != 1 {
		t.Fatal("allowed group refused")
	}
}
