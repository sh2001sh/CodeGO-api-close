package domain

import "testing"

func TestNormalizeMultiplierRemovesBinaryFloatArtifacts(t *testing.T) {
	if got := SubscriptionMultiplier(0.17); got != 1.7 {
		t.Fatalf("SubscriptionMultiplier(0.17) = %v, want 1.7", got)
	}
	if got := SubscriptionMultiplier(0.07); got != 0.7 {
		t.Fatalf("SubscriptionMultiplier(0.07) = %v, want 0.7", got)
	}
	if got := FormatMultiplier(0.900002); got != "0.9" {
		t.Fatalf("FormatMultiplier(0.900002) = %q, want 0.9", got)
	}
	if got := FormatMultiplier(0.1700000000002); got != "0.17" {
		t.Fatalf("FormatMultiplier(0.1700000000002) = %q, want 0.17", got)
	}
}
