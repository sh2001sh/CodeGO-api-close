package routing

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

func TestExactMarketplaceFactorCapsAndFrozenTargets(t *testing.T) {
	s := marketFixture()
	e := newEnv(s, Config{})
	p := s.Market.Channels[1]
	p.UserMultipliersExact = map[int64]string{1: "0.00000000000001"}
	s.Market.Channels[1] = p
	r := request("")
	r.Principal.MaxMarketplaceMultiplierPPM = 1
	first := mustPlan(t, e, r)[0]
	if first.MultiplierPPMExact != "0.00000000000001" {
		t.Fatalf("tiny positive factor was rounded: %#v", first)
	}
	p.UserMultipliersExact[1] = "1.00000000000001"
	s.Market.Channels[1] = p
	if _, err := e.planner.Plan(ctx, r); !errors.Is(err, ErrNoRoute) {
		t.Fatalf("fraction just above integral key cap was admitted: %v", err)
	}
	if first.MultiplierPPMExact != "0.00000000000001" {
		t.Fatal("admitted target changed with catalog override")
	}

	for _, factor := range []string{"0", "0.00000000000001"} {
		for _, status := range []string{"active", "suspended", "deleted"} {
			t.Run(factor+"/"+status, func(t *testing.T) {
				p.UserMultipliersExact[1] = factor
				s.Market.Channels[1] = p
				g := s.Market.Groups["market"]
				g.Status = status
				s.Market.Groups["market"] = g
				plan, err := e.planner.Plan(ctx, r)
				if status == "active" {
					if err != nil || len(plan) != 1 || plan[0].MultiplierPPMExact != factor {
						t.Fatalf("valid exact factor lost: %v %v", plan, err)
					}
				} else if !errors.Is(err, ErrNoRoute) || len(plan) != 0 {
					t.Fatalf("inactive zero/tiny channel routed: %v %v", plan, err)
				}
			})
		}
	}
}

func TestExactPoolCostOrderingAndCap(t *testing.T) {
	s := orderedPoolFixture("cost")
	e := newEnv(s, Config{})
	for id, value := range map[int64]string{1: "0.00000000000002", 2: "0.00000000000001", 3: "0"} {
		p := s.Market.Channels[id]
		p.UserMultipliersExact = map[int64]string{1: value}
		s.Market.Channels[id] = p
	}
	r := poolRequest(`{"user":"exact-session"}`)
	first := mustPlan(t, e, r)
	if got := channelOrder(first); !reflect.DeepEqual(got, []int64{3, 2, 1}) {
		t.Fatalf("fractional costs collapsed to zero/tiebreak: %v", got)
	}
	if first[0].MultiplierPPMExact != "0" || first[1].MultiplierPPMExact != "0.00000000000001" {
		t.Fatalf("zero and positive tiny factors indistinguishable: %v", first)
	}
	p := s.Market.Channels[1]
	p.UserMultipliersExact[1] = "1.00000000000001"
	s.Market.Channels[1] = p
	pool := s.Market.Pools["pool"]
	pool.MaxMultiplierPPM = 1
	s.Market.Pools["pool"] = pool
	if got := channelOrder(mustPlan(t, e, r)); !reflect.DeepEqual(got, []int64{3, 2}) {
		t.Fatalf("fraction just above pool cap admitted: %v", got)
	}
}

func TestExactMarketplaceInvalidOverrideFailsClosed(t *testing.T) {
	for _, bad := range []string{"", "NaN", "-0.1", "1/3"} {
		t.Run(bad, func(t *testing.T) {
			s := marketFixture()
			p := s.Market.Channels[1]
			p.UserMultipliersExact = map[int64]string{1: bad}
			s.Market.Channels[1] = p
			if _, err := newEnv(s, Config{}).planner.Plan(ctx, request("")); !errors.Is(err, ErrNoRoute) {
				t.Fatalf("invalid factor used fallback: %v", err)
			}
		})
	}
}

func TestExactMarketplaceUserOverridePrecedesFreeWindow(t *testing.T) {
	s := marketFixture()
	e := newEnv(s, Config{})
	p := s.Market.Channels[1]
	p.UserMultipliersExact = map[int64]string{1: "0.00000000000001"}
	p.Windows = []catalog.MarketMultiplierWindow{{StartsAt: e.clock.now(), EndsAt: e.clock.now().Add(time.Second), MultiplierPPM: 0}}
	s.Market.Channels[1] = p
	if target := mustPlan(t, e, request(""))[0]; target.MultiplierPPMExact != "0.00000000000001" {
		t.Fatalf("free window replaced actor override: %v", target)
	}
}
