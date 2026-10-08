package routing

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func setPoolCooldown(s *catalog.Snapshot, seconds int) {
	pool := s.Market.Pools["pool"]
	pool.FailureCooldownSeconds = seconds
	s.Market.Pools["pool"] = pool
}

func TestPoolCooldownFallbackExpiryAndTenantIsolation(t *testing.T) {
	s := orderedPoolFixture("priority")
	setPoolCooldown(s, 30)
	otherPool := s.Market.Pools["pool"]
	otherPool.OwnerUserID = 2
	s.Market.Pools["other-pool"] = otherPool
	e := newEnv(s, Config{})
	r := poolRequest(`{"user":"session"}`)
	first := mustPlan(t, e, r)
	if first[0].PersonalPoolGroup != "pool" || first[0].PoolFailureCooldown != 30*time.Second {
		t.Fatal("pool identity or configured duration missing from frozen target")
	}
	e.planner.Report(first[0], credFail)
	e.clock.advance(2 * time.Second) // global one-second backoff has expired
	if got := channelOrder(mustPlan(t, e, r)); !reflect.DeepEqual(got, []int64{2, 1}) {
		t.Fatalf("pool cooldown not applied before affinity: %v", got)
	}
	other := poolRequest("")
	other.Principal.UserID, other.Principal.KeyID, other.Principal.Group = 2, 10, "other-pool"
	if got := mustPlan(t, e, other)[0].ChannelID; got != 3 {
		t.Fatalf("user-configured cooldown leaked into another owner's pool: %d", got)
	}
	e.planner.Report(first[1], credFail)
	e.planner.Report(first[2], credFail)
	if plan, err := e.planner.Plan(ctx, r); !errors.Is(err, ErrNoRoute) || len(plan) != 0 {
		t.Fatalf("all-cooling fallback bypassed the configured cooldown: %v, %v", channelOrder(plan), err)
	}
	e.planner.Report(first[0], ok) // a late success must not shorten the timer
	e.clock.advance(28 * time.Second)
	if got := channelOrder(mustPlan(t, e, r)); !reflect.DeepEqual(got, []int64{3}) {
		t.Fatalf("first route failed to recover at exact expiry: %v", got)
	}
	e.clock.advance(2 * time.Second)
	if got := channelOrder(mustPlan(t, e, r)); !reflect.DeepEqual(got, []int64{3, 2, 1}) {
		t.Fatalf("remaining routes failed to recover: %v", got)
	}
}

func TestPoolCooldownIsModelScopedAndIgnoresRequestErrors(t *testing.T) {
	s := orderedPoolFixture("priority")
	setPoolCooldown(s, 30)
	for _, group := range []string{"a", "b", "c"} {
		s.Routes[group]["other"] = s.Routes[group]["gpt"]
	}
	e := newEnv(s, Config{})
	r := poolRequest("")
	first := mustPlan(t, e, r)[0]
	e.planner.Report(first, requestErr)
	if got := mustPlan(t, e, r)[0].ChannelID; got != 3 {
		t.Fatal("request error poisoned the pool")
	}
	e.planner.Report(first, modelFail)
	if got := mustPlan(t, e, r)[0].ChannelID; got != 2 {
		t.Fatal("model failure did not cool pool member")
	}
	other := poolRequest("")
	other.Model = "other"
	if got := mustPlan(t, e, other)[0].ChannelID; got != 3 {
		t.Fatal("pool model failure leaked to another model")
	}
}

func TestZeroPoolCooldownRetainsGlobalProtections(t *testing.T) {
	s := orderedPoolFixture("priority")
	setPoolCooldown(s, 0)
	e := newEnv(s, Config{})
	r := poolRequest("")
	first := mustPlan(t, e, r)
	e.planner.Report(first[0], credFail)
	if got := mustPlan(t, e, r)[0].ChannelID; got == first[0].ChannelID {
		t.Fatal("zero pool cooldown disabled global backoff")
	}
	e.clock.advance(time.Second)
	if got := mustPlan(t, e, r)[0].ChannelID; got != first[0].ChannelID {
		t.Fatal("zero pool cooldown added an extra timer")
	}
	for _, target := range first {
		e.planner.Report(target, gateway.AttemptResult{Retryable: true, Scope: gateway.ScopeCredential, Status: 401})
	}
	e.clock.advance(29 * time.Minute)
	if plan, err := e.planner.Plan(ctx, r); !errors.Is(err, ErrNoRoute) || len(plan) != 0 {
		t.Fatalf("all-cooling fallback bypassed the global authentication floor: %v, %v", channelOrder(plan), err)
	}
	e.clock.advance(time.Minute)
	if got := len(mustPlan(t, e, r)); got != 3 {
		t.Fatalf("authentication floor failed to expire: %d", got)
	}
}

func TestChangedPoolCooldownUsesLatestPolicy(t *testing.T) {
	s := orderedPoolFixture("priority")
	setPoolCooldown(s, 30)
	e := newEnv(s, Config{})
	r := poolRequest("")
	e.planner.Report(mustPlan(t, e, r)[0], credFail)
	e.clock.advance(2 * time.Second)
	if got := mustPlan(t, e, r)[0].ChannelID; got != 2 {
		t.Fatal("configured cooldown missing")
	}
	next := orderedPoolFixture("priority")
	setPoolCooldown(next, 0)
	e.swap(next)
	if got := mustPlan(t, e, r)[0].ChannelID; got != 3 {
		t.Fatal("disabling extra cooldown did not take effect on the next snapshot")
	}
}

func TestAllUnavailableModelsRespectGlobalCooldown(t *testing.T) {
	s := orderedPoolFixture("priority")
	for _, group := range []string{"a", "b", "c"} {
		s.Routes[group]["other"] = s.Routes[group]["gpt"]
	}
	e := newEnv(s, Config{})
	r := poolRequest("")
	for _, target := range mustPlan(t, e, r) {
		e.planner.Report(target, modelFail)
	}
	if plan, err := e.planner.Plan(ctx, r); !errors.Is(err, ErrNoRoute) || len(plan) != 0 {
		t.Fatalf("all-cooling fallback retried known unavailable models: %v, %v", channelOrder(plan), err)
	}
	other := poolRequest("")
	other.Model = "other"
	if got := len(mustPlan(t, e, other)); got != 3 {
		t.Fatal("unavailable-model exclusion blocked a supported model")
	}
	e.clock.advance(time.Second)
	if got := len(mustPlan(t, e, r)); got != 3 {
		t.Fatal("global unavailable-model cooldown failed to expire")
	}
}
