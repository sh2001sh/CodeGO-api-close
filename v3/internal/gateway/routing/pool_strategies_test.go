package routing

import (
	"errors"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

// User pools are persisted in the catalog as wildcard channel membership.
func projectedPoolFixture(strategy string) *catalog.Snapshot {
	s := orderedPoolFixture(strategy)
	routes := []catalog.Route{
		{ChannelID: 1, Weight: 1, Strategy: strategy},
		{ChannelID: 2, Weight: 3, Strategy: strategy},
		{ChannelID: 3, Weight: 1, Strategy: strategy},
	}
	s.Routes["pool"] = map[string][]catalog.Route{"gpt": routes, "*": routes}
	return s
}

func TestProjectedPoolStrategiesAndAttemptCap(t *testing.T) {
	for _, strategy := range []string{"weighted", "round_robin", "fill_first"} {
		t.Run(strategy, func(t *testing.T) {
			s := projectedPoolFixture(strategy)
			p := s.Market.Pools["pool"]
			p.MaxAttempts = 1
			s.Market.Pools["pool"] = p
			e := newEnv(s, Config{MaxAttempts: 2})
			counts := map[int64]int{}
			const n = 9000
			for i := 0; i < n; i++ {
				plan := mustPlan(t, e, poolRequest(""))
				if len(plan) != 1 {
					t.Fatalf("pool attempt cap ignored: %d", len(plan))
				}
				counts[plan[0].ChannelID]++
			}
			switch strategy {
			case "fill_first":
				if counts[1] != n {
					t.Fatalf("fill-first distribution: %v", counts)
				}
			case "round_robin":
				if counts[1] != n/3 || counts[2] != n/3 || counts[3] != n/3 {
					t.Fatalf("round-robin distribution: %v", counts)
				}
			case "weighted":
				if counts[2] < n*55/100 || counts[2] > n*65/100 {
					t.Fatalf("weighted distribution: %v", counts)
				}
			}
			p.MaxAttempts = 20
			s.Market.Pools["pool"] = p
			if got := len(mustPlan(t, e, poolRequest(""))); got != 2 {
				t.Fatalf("pool exceeded gateway attempt cap: %d", got)
			}
		})
	}
}

func TestProjectedPoolMemberPriorityIsAscending(t *testing.T) {
	for _, strategy := range []string{"weighted", "round_robin", "fill_first"} {
		s := projectedPoolFixture(strategy)
		for _, model := range []string{"gpt", "*"} {
			for i := range s.Routes["pool"][model] {
				s.Routes["pool"][model][i].Priority = -(3 - i)
			}
		}
		e := newEnv(s, Config{})
		if got := mustPlan(t, e, poolRequest(""))[0].ChannelID; got != 3 {
			t.Fatalf("%s failed to prioritize smallest member priority: %d", strategy, got)
		}
	}
}

func TestProjectedPoolOfficialMemberKeepsSupportedGroupPrice(t *testing.T) {
	s := projectedPoolFixture("fill_first")
	delete(s.Market.Channels, 1)
	s.Channels[1].Scope, s.Channels[1].Groups = "official", []string{"unsupported", "supported"}
	s.Groups = map[string]catalog.Group{"unsupported": {Multiplier: 0.1}, "supported": {Multiplier: 0.75}}
	s.Routes["unsupported"] = map[string][]catalog.Route{"other": {{ChannelID: 1, Weight: 1}}}
	s.Routes["supported"] = s.Routes["a"]
	p := s.Market.Pools["pool"]
	p.Members = []catalog.MarketPoolMember{{GroupID: "official:unsupported", CatalogGroupName: "unsupported"}, {GroupID: "official:supported", CatalogGroupName: "supported"}}
	p.GroupIDs = []string{"official:unsupported", "official:supported"}
	s.Market.Pools["pool"] = p
	r := poolRequest("")
	r.Principal.AllowedGroups = []string{"pool", "unsupported", "supported"}
	plan := mustPlan(t, newEnv(s, Config{}), r)
	if len(plan) != 1 || plan[0].Group != "supported" || plan[0].MultiplierPPM != 750000 {
		t.Fatalf("official group support or actual billed multiplier lost: %+v", channelOrder(plan))
	}
}

func TestProjectedPoolSkipsUnsupportedRevokedAndDeletedMembers(t *testing.T) {
	for _, strategy := range []string{"weighted", "round_robin", "fill_first", "priority", "cost", "score"} {
		for _, state := range []string{"unsupported", "paused", "private", "deleted", "blocked", "removed_member"} {
			t.Run(strategy+"/"+state, func(t *testing.T) {
				s := projectedPoolFixture(strategy)
				for _, group := range []string{"a", "b", "c"} {
					g := s.Market.Groups[group]
					switch state {
					case "unsupported":
						delete(s.Routes[group], "gpt")
					case "paused":
						g.Status = "paused"
					case "private":
						g.Visibility, g.OwnerUserID = "private", 10
					case "deleted":
						delete(s.Market.Groups, group)
					case "blocked":
						policy := s.Market.Channels[g.ChannelID]
						policy.Blocked = map[int64]bool{1: true}
						s.Market.Channels[g.ChannelID] = policy
					}
					if state != "deleted" {
						s.Market.Groups[group] = g
					}
				}
				if state == "removed_member" {
					p := s.Market.Pools["pool"]
					p.Members = nil
					s.Market.Pools["pool"] = p
				}
				if plan, err := newEnv(s, Config{}).planner.Plan(ctx, poolRequest("")); !errors.Is(err, ErrNoRoute) || len(plan) != 0 {
					t.Fatalf("stale projected pool routed %s member: %d targets, %v", state, len(plan), err)
				}
			})
		}
	}
}
