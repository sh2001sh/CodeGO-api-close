package catalog

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMarketFactorExactPrecedenceAndWindowBoundaries(t *testing.T) {
	now := time.Unix(1800000000, 0)
	p := MarketChannelPolicy{
		MultiplierPPM:        1000000,
		UserMultipliers:      map[int64]int64{2: 750000},
		UserMultipliersExact: map[int64]string{1: "0.00000000000001", 3: "0"},
		Windows: []MarketMultiplierWindow{
			{StartsAt: now, EndsAt: now.Add(time.Second), MultiplierPPM: 500000},
			{StartsAt: now.Add(-time.Second), EndsAt: now.Add(time.Second), MultiplierPPM: 250000},
			{StartsAt: now.Add(time.Second), EndsAt: now.Add(time.Hour), MultiplierPPM: 100000},
		},
	}
	for user, want := range map[int64]string{1: "0.00000000000001", 2: "750000", 3: "0", 4: "250000"} {
		if got := p.FactorExact(user, now); got != want {
			t.Fatalf("user %d factor=%q want=%q", user, got, want)
		}
	}
	if got := p.FactorExact(4, now.Add(time.Second)); got != "100000" {
		t.Fatalf("exclusive window end/inclusive start lost: %s", got)
	}
	if got := p.FactorExact(4, now.Add(time.Hour)); got != "1000000" {
		t.Fatalf("expired windows remained active: %s", got)
	}
	p.MultiplierPPMExact = "0"
	if got := p.FactorExact(4, now); got != "0" {
		t.Fatalf("exact zero was replaced by integral fallback: %s", got)
	}
}

func TestMarketExactPolicySurvivesSnapshotJSON(t *testing.T) {
	p := MarketChannelPolicy{MultiplierPPM: 1000000, MultiplierPPMExact: "1000000", UserMultipliersExact: map[int64]string{1: "0.000000000000000000000000000000000000000000000000000000001"}}
	blob, err := json.Marshal(MarketSnapshot{Channels: map[int64]MarketChannelPolicy{1: p}})
	if err != nil {
		t.Fatal(err)
	}
	var loaded MarketSnapshot
	if err := json.Unmarshal(blob, &loaded); err != nil {
		t.Fatal(err)
	}
	if got := loaded.Channels[1].FactorExact(1, time.Now()); got != p.UserMultipliersExact[1] {
		t.Fatalf("snapshot quantized exact override: %q", got)
	}
}
