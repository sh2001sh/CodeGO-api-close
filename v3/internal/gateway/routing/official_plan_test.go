package routing

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func scoredPlanFixture() *catalog.Snapshot {
	s := snapshotOf("fill_first", chanSpec{id: 1, priority: 100, creds: 2, weight: 1}, chanSpec{id: 2, priority: 50, creds: 1, weight: 1}, chanSpec{id: 3, creds: 1, weight: 1})
	s.Groups = map[string]catalog.Group{"default": {Multiplier: 0.4}}
	s.OfficialPools = map[string]catalog.OfficialPool{"default": {
		ID: 17, Name: "retained-official-pool", Group: "default", MultiplierWeight: 100,
		Members: []catalog.OfficialPoolMember{
			{ChannelID: 1, CostMultiplier: "0.9", Models: []string{"gpt"}, FaultDomain: "a"},
			{ChannelID: 2, CostMultiplier: "0.5", Models: []string{"gpt"}, FaultDomain: "b"},
			{ChannelID: 3, CostMultiplier: "0.7", ModelCostOverrides: map[string]json.Number{"gpt": "0.2"}, Models: []string{"gpt"}, FaultDomain: "c"},
		},
	}}
	return s
}

func TestOfficialScoredActualPlanOverridesNativePriorityAndKeepsPrice(t *testing.T) {
	s := scoredPlanFixture()
	e := newEnv(s, Config{})
	r := request(`{"user":"same-session"}`)
	first := mustPlan(t, e, r)
	if got := channelOrder(first); !reflect.DeepEqual(got, []int64{3, 2, 1}) {
		t.Fatalf("scored pool not consumed by actual planner: %v", got)
	}
	for _, target := range first {
		if target.Group != "default" || target.MultiplierPPM != 400000 {
			t.Fatal("procurement cost incorrectly changed customer pricing")
		}
		costs := map[int64]string{1: "0.9", 2: "0.5", 3: "0.2"}
		if target.RoutePoolID != 17 || target.ProcurementCostMultiplier != costs[target.ChannelID] {
			t.Fatal("accepted target lost selected pool/model-specific procurement provenance")
		}
	}
	pool := s.OfficialPools["default"]
	pool.Members[0].ModelCostOverrides = map[string]json.Number{"gpt": "0.01"}
	s.OfficialPools["default"] = pool
	if got := channelOrder(mustPlan(t, e, r)); !reflect.DeepEqual(got, []int64{1, 3, 2}) {
		t.Fatalf("old affinity overrode updated scored order: %v", got)
	}
	if first[0].ChannelID != 3 || first[0].MultiplierPPM != 400000 || first[0].ProcurementCostMultiplier != "0.2" {
		t.Fatal("a returned plan changed after catalog update")
	}
}

func TestOfficialScoredDomainIsolationSurvivesEveryFallback(t *testing.T) {
	for _, cooling := range []bool{false, true} {
		t.Run(map[bool]string{false: "ready", true: "all-cooling"}[cooling], func(t *testing.T) {
			s := scoredPlanFixture()
			pool := s.OfficialPools["default"]
			pool.Members[1].FaultDomain = "c"
			s.OfficialPools["default"] = pool
			e := newEnv(s, Config{MaxAttempts: 5})
			if cooling {
				for _, ch := range s.Channels {
					for _, cred := range ch.Credentials {
						e.planner.Report(gateway.Target{ChannelID: ch.ID, CredentialID: cred.ID, UpstreamModel: "gpt"}, credFail)
					}
				}
			}
			plan := mustPlan(t, e, request(""))
			if len(plan) != 2 || plan[0].ChannelID == plan[1].ChannelID ||
				(plan[0].ChannelID != 1 && plan[1].ChannelID != 1) {
				t.Fatalf("retry repeated channel/fault domain through fallback: %v", channelOrder(plan))
			}
		})
	}
}

func TestOfficialScoredScopeMismatchAndDisabledMemberDoNotLeak(t *testing.T) {
	s := scoredPlanFixture()
	pool := s.OfficialPools["default"]
	pool.ModelScope = "*" // literal star is not a wildcard in the source contract.
	s.OfficialPools["default"] = pool
	if first := mustPlan(t, newEnv(s, Config{}), request(""))[0]; first.ChannelID != 1 {
		t.Fatal("literal star pool scope matched an unrelated request")
	}
	pool.ModelScope = "GPT"
	pool.Members = pool.Members[1:] // compiler omitted an explicitly disabled member.
	s.OfficialPools["default"] = pool
	if plan := mustPlan(t, newEnv(s, Config{}), request("")); !reflect.DeepEqual(channelOrder(plan), []int64{3, 2}) {
		t.Fatalf("scope case matching or selected member set lost: %v", channelOrder(plan))
	}
	pool.Members = nil
	s.OfficialPools["default"] = pool
	if _, err := newEnv(s, Config{}).planner.Plan(ctx, request("")); err == nil {
		t.Fatal("empty matching scored pool silently used unselected native channels")
	}
}

func TestOfficialScoredActualReportChangesFuturePlansOnly(t *testing.T) {
	s := scoredPlanFixture()
	e := newEnv(s, Config{BaseBackoff: time.Nanosecond, MaxBackoff: time.Nanosecond})
	first := mustPlan(t, e, request(""))
	for range 2 {
		e.planner.Report(first[0], credFail)
	}
	e.clock.advance(time.Nanosecond)
	if plan := mustPlan(t, e, request("")); plan[0].ChannelID == 3 {
		t.Fatal("observed scored failures had no effect after credential cooldown expired")
	}
	if first[0].ChannelID != 3 {
		t.Fatal("report mutated an accepted retry plan")
	}
	e.clock.advance(15 * time.Minute)
	if first := mustPlan(t, e, request(""))[0]; first.ChannelID != 3 {
		t.Fatal("expired official health observations kept influencing selection")
	}
}

func TestOfficialScoredDomainsRemainIsolatedAcrossGroupRetries(t *testing.T) {
	s := scoredPlanFixture()
	primary := s.OfficialPools["default"]
	primary.Members = primary.Members[2:]
	s.OfficialPools["default"] = primary
	s.Groups["secondary"] = catalog.Group{Multiplier: 0.8}
	s.OfficialPools["secondary"] = catalog.OfficialPool{ID: 19, Group: "secondary", Members: []catalog.OfficialPoolMember{
		{ChannelID: 2, CostMultiplier: "0.01", Models: []string{"gpt"}, FaultDomain: "c"},
		{ChannelID: 1, CostMultiplier: "0.5", Models: []string{"gpt"}, FaultDomain: "independent"},
	}}
	r := request("")
	r.Principal.AllowedGroups, r.Principal.CrossGroupRetry = []string{"default", "secondary"}, true
	if plan := mustPlan(t, newEnv(s, Config{}), r); !reflect.DeepEqual(channelOrder(plan), []int64{3, 1}) {
		t.Fatalf("cross-group retry reused a configured fault domain: %v", channelOrder(plan))
	}
	// A native fallback still respects the default provider/upstream domain.
	delete(s.OfficialPools, "secondary")
	primary.Members[0].FaultDomain = ""
	s.OfficialPools["default"] = primary
	s.Routes["secondary"] = map[string][]catalog.Route{"gpt": {{ChannelID: 1, Priority: 100, Weight: 1, Strategy: "fill_first"}, {ChannelID: 2, Weight: 1, Strategy: "fill_first"}}}
	s.Channels[2].BaseURL = "https://independent.example.test"
	if plan := mustPlan(t, newEnv(s, Config{}), r); !reflect.DeepEqual(channelOrder(plan), []int64{3, 2}) {
		t.Fatalf("native fallback retried the same physical upstream: %v", channelOrder(plan))
	}
}

func TestPersonalPoolConsumesOfficialMemberScoringAndProcurement(t *testing.T) {
	s := scoredPlanFixture()
	s.Market.Pools = map[string]catalog.MarketPoolPolicy{"pool": {
		OwnerUserID: 1, Strategy: "priority", MaxAttempts: 3,
		Members: []catalog.MarketPoolMember{{GroupID: "official:default", CatalogGroupName: "default"}},
	}}
	pool := s.OfficialPools["default"]
	pool.Members = pool.Members[1:] // explicitly disabled channel1 must not reenter through a personal pool.
	s.OfficialPools["default"] = pool
	r := poolRequest("")
	r.Principal.AllowedGroups = []string{"pool", "default"}
	plan := mustPlan(t, newEnv(s, Config{}), r)
	if !reflect.DeepEqual(channelOrder(plan), []int64{3, 2}) {
		t.Fatalf("personal pool bypassed official member scoring/filtering: %v", channelOrder(plan))
	}
	for _, target := range plan {
		if target.Group != "default" || target.MultiplierPPM != 400000 || target.RoutePoolID != 17 || target.ProcurementCostMultiplier == "" {
			t.Fatal("personal pool lost official member price/procurement selection")
		}
	}
}

func TestOfficialActualReportUsesMeasuredTTFTAndCacheFields(t *testing.T) {
	s := scoredPlanFixture()
	pool := s.OfficialPools["default"]
	pool.Members = pool.Members[:2]
	for i := range pool.Members {
		pool.Members[i].CostMultiplier = "1"
	}
	pool.MultiplierWeight, pool.TTFTWeight = 0, 100
	s.OfficialPools["default"] = pool
	e := newEnv(s, Config{})
	for range 10 {
		e.planner.Report(gateway.Target{ChannelID: 1, UpstreamModel: "gpt"}, gateway.AttemptResult{OK: true, TTFT: 100 * time.Millisecond, PromptTokens: 100})
		e.planner.Report(gateway.Target{ChannelID: 2, UpstreamModel: "gpt"}, gateway.AttemptResult{OK: true, TTFT: 140 * time.Millisecond, PromptTokens: 100, CachedTokens: 100})
	}
	if target := mustPlan(t, e, request(""))[0]; target.ChannelID != 1 {
		t.Fatal("actual Report did not feed observed first-event latency into scored Plan")
	}
	pool.TTFTWeight, pool.CacheWeight = 0, 100
	s.OfficialPools["default"] = pool
	if target := mustPlan(t, e, request(""))[0]; target.ChannelID != 2 {
		t.Fatal("actual Report did not feed observed cache usage into scored Plan")
	}
}
