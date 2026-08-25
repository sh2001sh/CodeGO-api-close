package app

import (
	"testing"
	"time"

	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
)

func TestMarketplaceAutoProbeDueUsesStableJitter(t *testing.T) {
	last := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	channel := marketplaceschema.Channel{
		ID:                       "channel-a",
		AutoProbeIntervalMinutes: 10,
		AutoProbeLastAt:          &last,
	}
	jitter := autoProbeJitter(channel.ID, channel.AutoProbeIntervalMinutes)
	if jitter < 0 || jitter > 30*time.Second {
		t.Fatalf("jitter = %v, want [0, 30s]", jitter)
	}
	if got := autoProbeJitter(channel.ID, channel.AutoProbeIntervalMinutes); got != jitter {
		t.Fatalf("jitter is not stable: first=%v second=%v", jitter, got)
	}
	if marketplaceAutoProbeDue(channel, last.Add(10*time.Minute+jitter-time.Nanosecond)) {
		t.Fatal("probe became due before interval plus jitter")
	}
	if !marketplaceAutoProbeDue(channel, last.Add(10*time.Minute+jitter)) {
		t.Fatal("probe did not become due at interval plus jitter")
	}
}

func TestMarketplaceAutoProbeDueRunsInitialProbeImmediately(t *testing.T) {
	channel := marketplaceschema.Channel{ID: "initial", AutoProbeIntervalMinutes: 10}
	if !marketplaceAutoProbeDue(channel, time.Now().UTC()) {
		t.Fatal("initial probe should be due")
	}
}
