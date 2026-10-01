package routing

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func officialFixture() (*catalog.Snapshot, catalog.OfficialPool) {
	snap := &catalog.Snapshot{Channels: map[int64]*catalog.Channel{}}
	pool := catalog.OfficialPool{ID: 7, Group: "default", MultiplierWeight: 35, TTFTWeight: 25, CacheWeight: 15, SuccessWeight: 25}
	for id := int64(1); id <= 3; id++ {
		snap.Channels[id] = &catalog.Channel{ID: id, Provider: "openai", BaseURL: "https://shared.example/v1", Credentials: []catalog.Credential{{ID: id, ChannelID: id}}}
		pool.Members = append(pool.Members, catalog.OfficialPoolMember{ChannelID: id, CostMultiplier: "1", Models: []string{"gpt-4"}})
	}
	snap.Channels[3].BaseURL = "https://isolated.example/v1"
	return snap, pool
}

func bestOfficial(t *testing.T, snap *catalog.Snapshot, pool catalog.OfficialPool, health *officialHealth, now time.Time) int64 {
	t.Helper()
	idx, _ := officialPoolIndex(snap, pool, "gpt-4", health, now)
	if idx == nil || len(idx.tiers) == 0 {
		t.Fatal("no index")
	}
	return idx.tiers[0].entries[0].ch.ID
}

func TestOfficialScoringUsesExactOverrideAndObservedFailures(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	snap, pool := officialFixture()
	pool.Members[0].CostMultiplier = "2"
	pool.Members[1].CostMultiplier = "3"
	pool.Members[1].ModelCostOverrides = map[string]json.Number{"gpt-4": "0.25"}
	health := newOfficialHealth()
	if best := bestOfficial(t, snap, pool, health, now); best != 2 {
		t.Fatalf("model procurement override not used: %d", best)
	}
	for range 10 {
		health.observe(gateway.Target{ChannelID: 2, UpstreamModel: "gpt-4"}, gateway.AttemptResult{Retryable: true, Scope: gateway.ScopeModel}, OfficialAttemptMetrics{}, now)
	}
	if best := bestOfficial(t, snap, pool, health, now); best != 3 {
		t.Fatalf("unstable cheap member beats healthy member: %d", best)
	}
	if best := bestOfficial(t, snap, pool, health, now.Add(16*time.Minute)); best != 2 {
		t.Fatalf("expired failures continue penalizing: %d", best)
	}
	pool.Members[0].CostMultiplier, pool.Members[1].CostMultiplier, pool.Members[2].CostMultiplier = "1.00000000000000000002", "1.00000000000000000001", "2"
	pool.Members[1].ModelCostOverrides = nil
	if best := bestOfficial(t, snap, pool, nil, now); best != 2 {
		t.Fatalf("exact cost tie rounded away: %d", best)
	}
}

func TestOfficialLatencyCacheAndWeightsChangeOrdering(t *testing.T) {
	now := time.Now()
	snap, pool := officialFixture()
	pool.Members = pool.Members[:2]
	health := newOfficialHealth()
	for range 20 {
		health.observe(gateway.Target{ChannelID: 1, UpstreamModel: "gpt-4"}, gateway.AttemptResult{OK: true}, OfficialAttemptMetrics{TTFT: 100 * time.Millisecond, PromptTokens: 100}, now)
		health.observe(gateway.Target{ChannelID: 2, UpstreamModel: "gpt-4"}, gateway.AttemptResult{OK: true}, OfficialAttemptMetrics{TTFT: 140 * time.Millisecond, PromptTokens: 100, CachedTokens: 100}, now)
	}
	pool.MultiplierWeight, pool.TTFTWeight, pool.CacheWeight, pool.SuccessWeight = 0, 100, 0, 0
	if best := bestOfficial(t, snap, pool, health, now); best != 1 {
		t.Fatalf("TTFT weighting ignored: %d", best)
	}
	pool.TTFTWeight, pool.CacheWeight = 0, 100
	if best := bestOfficial(t, snap, pool, health, now); best != 2 {
		t.Fatalf("cache weighting ignored: %d", best)
	}
	if score := officialScore(pool, "1", health.read(2, "gpt-4", now), now); score >= officialScore(pool, "1", health.read(1, "gpt-4", now), now) {
		t.Fatal("cached real usage failed to lower cost score")
	}
}

func TestOfficialFaultDomainsExcludeSiblingAndOwnCredentials(t *testing.T) {
	snap, pool := officialFixture()
	_, selections := officialPoolIndex(snap, pool, "gpt-4", nil, time.Now())
	targets := []gateway.Target{{ChannelID: 1, CredentialID: 11}}
	if officialDomainAllowed(selections, targets, 1) || officialDomainAllowed(selections, targets, 2) || !officialDomainAllowed(selections, targets, 3) {
		t.Fatal("retry did not isolate shared host/channel")
	}
	pool.Members[0].FaultDomain, pool.Members[2].FaultDomain = " Configured ", "configured"
	_, selections = officialPoolIndex(snap, pool, "gpt-4", nil, time.Now())
	if officialDomainAllowed(selections, targets, 3) {
		t.Fatal("configured fault domain not normalized or excluded")
	}
	pool.ModelScope = "*"
	if idx, _ := officialPoolIndex(snap, pool, "gpt-4", nil, time.Now()); idx != nil {
		t.Fatal("literal source star was treated as wildcard")
	}
}

func TestOfficialHealthIgnoresClientErrorsAndAbsentMeasurements(t *testing.T) {
	now := time.Now()
	health := newOfficialHealth()
	target := gateway.Target{ChannelID: 1, UpstreamModel: "gpt-4"}
	health.observe(target, gateway.AttemptResult{Scope: gateway.ScopeRequest}, OfficialAttemptMetrics{TTFT: time.Second, PromptTokens: 100, CachedTokens: 20}, now)
	if state := health.read(1, "gpt-4", now); state.w5.requests != 0 || state.p95(now) != 0 {
		t.Fatalf("client error poisoned health %+v", state)
	}
	health.observe(target, gateway.AttemptResult{OK: true}, OfficialAttemptMetrics{}, now)
	if state := health.read(1, "gpt-4", now); state.w5.successes != 1 || state.p95(now) != 0 || state.cacheSamples != 0 {
		t.Fatalf("synthetic latency/cache measurements %+v", state)
	}
}
