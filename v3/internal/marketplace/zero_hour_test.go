package marketplace

import "testing"

func TestZeroHourProbabilityIntegerBoundaries(t *testing.T) {
	for _, tc := range []struct{ points, want int64 }{{-1, 1000}, {0, 1000}, {1, 1049}, {999, 49951}, {1000, 50000}, {1001, 50000}} {
		if got := zeroHourDrawThreshold(tc.points); got != tc.want {
			t.Fatalf("points %d: threshold %d, want %d", tc.points, got, tc.want)
		}
	}
}
