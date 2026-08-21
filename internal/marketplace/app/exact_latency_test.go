package app

import "testing"

func TestPercentileUsesExactNearestRank(t *testing.T) {
	values := []float64{900, 100, 500, 300, 700}
	if got := percentile(values, 0.50); got != 500 {
		t.Fatalf("p50 = %v, want 500", got)
	}
	if got := percentile(values, 0.95); got != 900 {
		t.Fatalf("p95 = %v, want 900", got)
	}
}
