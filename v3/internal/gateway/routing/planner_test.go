package routing

import (
	"context"
	"errors"
	"math"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

var ctx = context.Background()

func mustPlan(t *testing.T, e *env, req *gateway.Request) []gateway.Target {
	t.Helper()
	ts, err := e.planner.Plan(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func TestPriorityOrderAndDistinctChannels(t *testing.T) {
	e := newEnv(snapshotOf("weighted", chanSpec{id: 1, priority: 10, weight: 1, creds: 2}, chanSpec{id: 2, priority: 5, weight: 1, creds: 1}), Config{})
	ts := mustPlan(t, e, request(""))
	if len(ts) != 3 || ts[0].ChannelID != 1 {
		t.Fatalf("plan = %v", credIDs(ts))
	}
	// Higher tier first, then its second key, then the lower tier.
	if ts[1].ChannelID != 1 || ts[2].ChannelID != 2 {
		t.Fatalf("expected both keys of the top tier before the lower tier, got %v", credIDs(ts))
	}
}

func TestDistinctChannelsBeforeExtraKeys(t *testing.T) {
	e := newEnv(snapshotOf("weighted", chanSpec{id: 1, weight: 1, creds: 3}, chanSpec{id: 2, weight: 1, creds: 3}), Config{})
	for i := 0; i < 50; i++ {
		ts := mustPlan(t, e, request(""))
		if ts[0].ChannelID == ts[1].ChannelID {
			t.Fatalf("first two candidates share channel %d: %v", ts[0].ChannelID, credIDs(ts))
		}
	}
}

func TestWeightedDistribution(t *testing.T) {
	e := newEnv(snapshotOf("weighted", chanSpec{id: 1, weight: 1, creds: 1}, chanSpec{id: 2, weight: 3, creds: 1}), Config{})
	const n = 40_000
	first := map[int64]int{}
	for i := 0; i < n; i++ {
		first[mustPlan(t, e, request(""))[0].ChannelID]++
	}
	share := float64(first[2]) / n
	if math.Abs(share-0.75) > 0.05*0.75 {
		t.Fatalf("weight-3 channel got %.3f of first picks; want 0.75 ±5%%", share)
	}
}

func TestRoundRobinAndFillFirst(t *testing.T) {
	rr := newEnv(snapshotOf("round_robin", chanSpec{id: 1, weight: 1, creds: 1}, chanSpec{id: 2, weight: 1, creds: 1}, chanSpec{id: 3, weight: 1, creds: 1}), Config{})
	seen := map[int64]int{}
	for i := 0; i < 300; i++ {
		seen[mustPlan(t, rr, request(""))[0].ChannelID]++
	}
	for id := int64(1); id <= 3; id++ {
		if seen[id] != 100 {
			t.Fatalf("round robin first picks = %v; want 100 each", seen)
		}
	}
	ff := newEnv(snapshotOf("fill_first", chanSpec{id: 1, weight: 1, creds: 1}, chanSpec{id: 2, weight: 1, creds: 1}), Config{})
	for i := 0; i < 20; i++ {
		if got := mustPlan(t, ff, request(""))[0].ChannelID; got != 1 {
			t.Fatalf("fill_first picked channel %d", got)
		}
	}
}

func TestCredentialCooldownAndBackoff(t *testing.T) {
	e := newEnv(snapshotOf("fill_first", chanSpec{id: 1, weight: 1, creds: 1}, chanSpec{id: 2, weight: 1, creds: 1}), Config{})
	a := mustPlan(t, e, request(""))[0]
	e.planner.Report(a, credFail) // streak 1: 1 s
	if got := mustPlan(t, e, request(""))[0].CredentialID; got == a.CredentialID {
		t.Fatal("cooling credential still planned first")
	}
	e.clock.advance(1100 * time.Millisecond)
	if got := mustPlan(t, e, request(""))[0].CredentialID; got != a.CredentialID {
		t.Fatalf("credential did not recover after 1 s, got %d", got)
	}
	e.planner.Report(a, credFail) // streak 2: 2 s
	e.clock.advance(1500 * time.Millisecond)
	if got := mustPlan(t, e, request(""))[0].CredentialID; got == a.CredentialID {
		t.Fatal("second failure should back off to 2 s")
	}
	e.clock.advance(time.Second)
	e.planner.Report(a, ok) // resets the streak
	e.planner.Report(a, credFail)
	e.clock.advance(1100 * time.Millisecond)
	if got := mustPlan(t, e, request(""))[0].CredentialID; got != a.CredentialID {
		t.Fatal("success did not reset backoff to 1 s")
	}
}

func TestRetryAfterAndAuthFloor(t *testing.T) {
	e := newEnv(snapshotOf("fill_first", chanSpec{id: 1, weight: 1, creds: 1}, chanSpec{id: 2, weight: 1, creds: 1}), Config{})
	a := mustPlan(t, e, request(""))[0]
	e.planner.Report(a, gateway.AttemptResult{Retryable: true, Scope: gateway.ScopeCredential, Status: 429, RetryAfter: 10 * time.Second})
	e.clock.advance(9 * time.Second)
	if mustPlan(t, e, request(""))[0].CredentialID == a.CredentialID {
		t.Fatal("Retry-After of 10 s not honored at 9 s")
	}
	e.clock.advance(2 * time.Second)
	if mustPlan(t, e, request(""))[0].CredentialID != a.CredentialID {
		t.Fatal("credential still cooling after Retry-After")
	}

	e.planner.Report(a, gateway.AttemptResult{Retryable: true, Scope: gateway.ScopeCredential, Status: 401})
	e.clock.advance(29 * time.Minute)
	if mustPlan(t, e, request(""))[0].CredentialID == a.CredentialID {
		t.Fatal("401 must cool for at least 30 min")
	}
}

func TestModelScopedCooldown(t *testing.T) {
	snap := snapshotOf("fill_first", chanSpec{id: 1, weight: 1, creds: 1}, chanSpec{id: 2, weight: 1, creds: 1})
	snap.Routes["default"]["other"] = snap.Routes["default"]["gpt"]
	e := newEnv(snap, Config{})
	a := mustPlan(t, e, request(""))[0]
	e.planner.Report(a, modelFail)
	if mustPlan(t, e, request(""))[0].CredentialID == a.CredentialID {
		t.Fatal("model-scoped failure not applied to its model")
	}
	other := request(`{"model":"other"}`)
	other.Model = "other"
	if mustPlan(t, e, other)[0].CredentialID != a.CredentialID {
		t.Fatal("model-scoped failure leaked to another model")
	}
	e.planner.Report(a, requestErr)
	if mustPlan(t, e, other)[0].CredentialID != a.CredentialID {
		t.Fatal("request-scoped error must not cool anything")
	}
}

func TestAllCoolingFallsBackBySoonestRecovery(t *testing.T) {
	e := newEnv(snapshotOf("fill_first", chanSpec{id: 1, weight: 1, creds: 1}, chanSpec{id: 2, weight: 1, creds: 1}), Config{})
	c1 := gateway.Target{CredentialID: 100}
	c2 := gateway.Target{CredentialID: 200}
	e.planner.Report(c1, gateway.AttemptResult{Scope: gateway.ScopeCredential, RetryAfter: 10 * time.Second})
	e.planner.Report(c2, gateway.AttemptResult{Scope: gateway.ScopeCredential, RetryAfter: 5 * time.Second})
	if got := credIDs(mustPlan(t, e, request(""))); !slices.Equal(got, []int64{200, 100}) {
		t.Fatalf("fallback order = %v; want [200 100]", got)
	}
}

func TestSessionAffinity(t *testing.T) {
	e := newEnv(snapshotOf("weighted", chanSpec{id: 1, weight: 1, creds: 1}, chanSpec{id: 2, weight: 1, creds: 1}, chanSpec{id: 3, weight: 1, creds: 1}), Config{})
	body := `{"model":"gpt","metadata":{"user_id":"session-42"}}`
	pinned := mustPlan(t, e, request(body))[0]
	for i := 0; i < 100; i++ {
		if got := mustPlan(t, e, request(body))[0].CredentialID; got != pinned.CredentialID {
			t.Fatalf("affinity lost on call %d: %d != %d", i, got, pinned.CredentialID)
		}
	}
	e.planner.Report(pinned, credFail)
	moved := mustPlan(t, e, request(body))[0]
	if moved.CredentialID == pinned.CredentialID {
		t.Fatal("affinity pinned a cooling credential")
	}
	if got := mustPlan(t, e, request(body))[0].CredentialID; got != moved.CredentialID {
		t.Fatal("affinity did not follow the replacement credential")
	}
	// Another API key with the same session string must not share the entry.
	otherKey := request(body)
	otherKey.Principal.KeyID = 10
	if h1, _ := e.planner.aff.sessionKey(request(body)); func() bool { h2, _ := e.planner.aff.sessionKey(otherKey); return h1 == h2 }() {
		t.Fatal("session keys collide across API keys")
	}
}

// v2 8444c4dbf: a retry re-ran a different check and rejected the route the
// first attempt had chosen. In v3 the plan is computed once; later reports and
// snapshot swaps never change a plan already handed to the gateway.
func TestPlanIsStableAcrossReportsAndSnapshotSwap(t *testing.T) {
	e := newEnv(snapshotOf("fill_first", chanSpec{id: 1, weight: 1, creds: 1}, chanSpec{id: 2, weight: 1, creds: 1}), Config{})
	plan := mustPlan(t, e, request(""))
	before := slices.Clone(plan)
	e.planner.Report(plan[0], credFail)
	e.swap(snapshotOf("fill_first", chanSpec{id: 3, weight: 1, creds: 1})) // channels 1 and 2 removed
	if !slices.EqualFunc(plan, before, func(a, b gateway.Target) bool { return reflect.DeepEqual(a, b) }) || plan[1].Secret == "" || plan[1].BaseURL == "" {
		t.Fatalf("plan mutated after report/swap: %+v", plan)
	}
	if got := mustPlan(t, e, request(""))[0].ChannelID; got != 3 {
		t.Fatalf("new snapshot not used for new plans, got channel %d", got)
	}
}

func TestNoRouteAndWildcard(t *testing.T) {
	e := newEnv(snapshotOf("weighted", chanSpec{id: 1, weight: 1, creds: 1}), Config{})
	missing := request("")
	missing.Model = "unknown"
	if _, err := e.planner.Plan(ctx, missing); !errors.Is(err, ErrNoRoute) {
		t.Fatalf("err = %v; want ErrNoRoute", err)
	}
	snap := snapshotOf("weighted", chanSpec{id: 1, weight: 1, creds: 1, mapping: map[string]string{"unknown": "upstream-x"}})
	snap.Routes["default"]["*"] = snap.Routes["default"]["gpt"]
	e.swap(snap)
	ts := mustPlan(t, e, missing)
	if ts[0].ChannelID != 1 || ts[0].UpstreamModel != "upstream-x" {
		t.Fatalf("wildcard route = %+v", ts[0])
	}
	var nilSnap *catalog.Snapshot
	e.swap(nilSnap)
	if _, err := e.planner.Plan(ctx, request("")); !errors.Is(err, ErrNoRoute) {
		t.Fatalf("nil snapshot err = %v", err)
	}
}

func TestConcurrentPlanAndReport(t *testing.T) {
	var specs []chanSpec
	for i := 1; i <= 20; i++ {
		specs = append(specs, chanSpec{id: i, priority: i % 3, weight: i, creds: 3})
	}
	e := newEnv(snapshotOf("weighted", specs...), Config{})
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				ts, err := e.planner.Plan(ctx, request(`{"model":"gpt","user":"u`+string(rune('a'+g))+`"}`))
				if err != nil || len(ts) == 0 {
					t.Error("empty plan under concurrency")
					return
				}
				res := ok
				if i%3 == 0 {
					res = credFail
				}
				e.planner.Report(ts[0], res)
				if i%500 == 0 {
					e.swap(snapshotOf("weighted", specs...))
				}
			}
		}(g)
	}
	wg.Wait()
}
