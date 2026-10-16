package main

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

func TestCPUSamplingFiniteAtClockBoundaries(t *testing.T) {
	for _, tc := range []struct{ cpu, elapsed, want float64 }{
		{0, 0, 0}, {1, 0, 0}, {0, -1, 0}, {-1, 1, 0},
		{math.NaN(), 1, 0}, {1, math.NaN(), 0}, {math.Inf(1), 1, 0}, {1, math.Inf(1), 0},
		{math.MaxFloat64, math.SmallestNonzeroFloat64, 0}, {0, 1, 0}, {2, .5, 4},
	} {
		got := finiteCPURatio(tc.cpu, tc.elapsed)
		if got != tc.want || math.IsNaN(got) || math.IsInf(got, 0) {
			t.Fatalf("ratio(%v,%v)=%v; want %v", tc.cpu, tc.elapsed, got, tc.want)
		}
		if _, err := json.Marshal(processMetrics{CPUCoresPeak: got, CPUCoresAverage: got}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestImmediateProcessSamplingSerializes(t *testing.T) {
	var metrics processMetrics
	stop := sampleProcess(os.Getpid(), &metrics)
	stop()
	if metrics.Error != "" {
		t.Fatal(metrics.Error)
	}
	if metrics.Samples < 1 {
		t.Fatal("no process sample recorded")
	}
	if _, err := json.Marshal(metrics); err != nil {
		t.Fatal(err)
	}
}
