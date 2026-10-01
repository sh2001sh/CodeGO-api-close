//go:build !race

package routing

import (
	"slices"
	"testing"
	"time"
)

// 10,000 credentials: 1,000 channels x 10 keys in one group/model.
func largeEnv() *env {
	specs := make([]chanSpec, 0, 1000)
	for i := 1; i <= 1000; i++ {
		specs = append(specs, chanSpec{id: i, priority: i % 4, weight: 1 + i%7, creds: 10})
	}
	return newEnv(snapshotOf("weighted", specs...), Config{})
}

func BenchmarkPlan(b *testing.B) {
	e := largeEnv()
	req := request(`{"model":"gpt","metadata":{"user_id":"bench"}}`)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := e.planner.Plan(ctx, req); err != nil {
			b.Fatal(err)
		}
	}
}

// Acceptance (tasks.md M1): one Plan over 10k credentials is p99 <= 50 µs.
// Excluded under -race, whose instrumentation distorts timing.
func TestPlanLatencyP99(t *testing.T) {
	e := largeEnv()
	req := request(`{"model":"gpt","user":"latency"}`)
	for i := 0; i < 2000; i++ { // warm the index cache and allocator
		_, _ = e.planner.Plan(ctx, req)
	}
	const n = 20_000
	samples := make([]time.Duration, n)
	for i := range samples {
		start := time.Now()
		if _, err := e.planner.Plan(ctx, req); err != nil {
			t.Fatal(err)
		}
		samples[i] = time.Since(start)
	}
	slices.Sort(samples)
	p50, p99 := samples[n/2], samples[n*99/100]
	if samples[n-1] == 0 {
		// Some platforms (Windows) cannot time a sub-microsecond call; a
		// reading of 0 would be a false pass. Run this test on Linux.
		t.Skip("monotonic clock too coarse to measure per-call latency")
	}
	t.Logf("Plan over 10k credentials: p50=%v p99=%v max=%v", p50, p99, samples[n-1])
	if p99 > 50*time.Microsecond {
		t.Fatalf("p99 = %v; want <= 50µs", p99)
	}
}
