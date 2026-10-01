package routing

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func orderedPoolFixture(strategy string) *catalog.Snapshot {
	s := snapshotOf("fill_first", chanSpec{id: 1, creds: 1, weight: 1}, chanSpec{id: 2, creds: 1, weight: 1}, chanSpec{id: 3, creds: 1, weight: 1})
	s.Market = catalog.MarketSnapshot{Groups: map[string]catalog.MarketGroupPolicy{}, Channels: map[int64]catalog.MarketChannelPolicy{}, Pools: map[string]catalog.MarketPoolPolicy{}}
	for id, group := range map[int64]string{1: "a", 2: "b", 3: "c"} {
		s.Channels[id].Scope = "marketplace"
		s.Market.Groups[group] = catalog.MarketGroupPolicy{ID: group, ChannelID: id, Status: "active", Visibility: "public"}
		s.Market.Channels[id] = catalog.MarketChannelPolicy{GroupName: group, MultiplierPPM: id * 1000000, UserMultipliers: map[int64]int64{}}
		s.Routes[group] = map[string][]catalog.Route{"gpt": {{ChannelID: id, Weight: 1, Strategy: "weighted"}}}
	}
	s.Market.Pools["pool"] = catalog.MarketPoolPolicy{OwnerUserID: 1, Strategy: strategy, MaxAttempts: 3, Members: []catalog.MarketPoolMember{
		{GroupID: "a", CatalogGroupName: "a", Priority: 3}, {GroupID: "b", CatalogGroupName: "b", Priority: 2}, {GroupID: "c", CatalogGroupName: "c", Priority: 1}}}
	return s
}

func poolRequest(body string) *gateway.Request {
	r := request(body)
	r.Principal.Group = "pool"
	return r
}

func channelOrder(plan []gateway.Target) []int64 {
	ids := make([]int64, len(plan))
	for i, target := range plan {
		ids[i] = target.ChannelID
	}
	return ids
}

func TestActualPoolPriorityCostAndScoreOrdering(t *testing.T) {
	for _, strategy := range []string{"priority", "cost", "score"} {
		t.Run(strategy, func(t *testing.T) {
			s := orderedPoolFixture(strategy)
			// An observing score of 100 must follow both mature scores.
			g := s.Market.Groups["c"]
			g.Score, g.HasScore = 40, true
			s.Market.Groups["c"] = g
			g = s.Market.Groups["b"]
			g.Score, g.HasScore = 80, true
			s.Market.Groups["b"] = g
			g = s.Market.Groups["a"]
			g.Score = 100
			s.Market.Groups["a"] = g
			want := []int64{3, 2, 1}
			switch strategy {
			case "cost":
				want = []int64{1, 2, 3}
			case "score":
				want = []int64{2, 3, 1}
			}
			if got := channelOrder(mustPlan(t, newEnv(s, Config{}), poolRequest(""))); !reflect.DeepEqual(got, want) {
				t.Fatalf("%s planner order=%v want=%v", strategy, got, want)
			}
		})
	}
}

func TestPoolCostUsesActorAndLocalWindowExpiryBeforeAffinity(t *testing.T) {
	s := orderedPoolFixture("cost")
	e := newEnv(s, Config{})
	policy := s.Market.Channels[2]
	policy.Windows = []catalog.MarketMultiplierWindow{{StartsAt: e.clock.now(), EndsAt: e.clock.now().Add(time.Second), MultiplierPPM: 0}}
	s.Market.Channels[2] = policy
	policy = s.Market.Channels[3]
	policy.UserMultipliers[1] = 500000
	s.Market.Channels[3] = policy
	r := poolRequest(`{"user":"sticky-session"}`)
	first := mustPlan(t, e, r)
	if got := channelOrder(first); !reflect.DeepEqual(got, []int64{2, 3, 1}) || first[0].MultiplierPPM != 0 {
		t.Fatalf("effective actor/window costs ignored: %v", got)
	}
	e.clock.advance(time.Second)
	if got := channelOrder(mustPlan(t, e, r)); !reflect.DeepEqual(got, []int64{3, 1, 2}) {
		t.Fatalf("expired cost window or cross-group affinity overrode strategy: %v", got)
	}
	if first[0].MultiplierPPM != 0 {
		t.Fatal("previously frozen plan changed after expiry")
	}
}

func TestPoolOfficialMembershipChecksRevocationAndPrice(t *testing.T) {
	s := orderedPoolFixture("cost")
	s.Channels[1].Scope, s.Channels[1].Groups = "official", []string{"official-group"}
	delete(s.Market.Channels, 1)
	s.Groups = map[string]catalog.Group{"official-group": {Multiplier: 0.25}}
	s.Routes["official-group"] = s.Routes["a"]
	p := s.Market.Pools["pool"]
	p.Members[0] = catalog.MarketPoolMember{GroupID: "official:official-group", CatalogGroupName: "official-group"}
	s.Market.Pools["pool"] = p
	r := poolRequest("")
	r.Principal.AllowedGroups = []string{"pool", "official-group"}
	e := newEnv(s, Config{})
	first := mustPlan(t, e, r)[0]
	if first.ChannelID != 1 || first.Group != "official-group" || first.MultiplierPPM != 250000 {
		t.Fatal("official pool member did not preserve its own price")
	}
	s.AccountProfiles = map[int64]catalog.AccountProfile{1: {AllowedGroups: []string{"pool"}}}
	if plan := mustPlan(t, e, r); len(plan) != 2 || plan[0].ChannelID == 1 {
		t.Fatal("revoked official member remained authorized through owned pool")
	}
	s.AccountProfiles[1] = catalog.AccountProfile{AllowedGroups: []string{}}
	if _, err := e.planner.Plan(ctx, r); !errors.Is(err, ErrNoRoute) {
		t.Fatal("revoked pool grant remained usable")
	}
}
