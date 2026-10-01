package ledger

import (
	"math"
	"testing"
	"time"
)

func TestWalletRewardExactReleaseAgeBoundaries(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		age                      time.Duration
		original, consumed, want int64
	}{
		{23 * time.Hour, 100, 0, 100}, {24 * time.Hour, 100, 0, 100},
		{48 * time.Hour, 101, 0, 51}, {48 * time.Hour, 101, 50, 1},
		{72*time.Hour - time.Nanosecond, math.MaxInt64, 0, 53376},
		{72 * time.Hour, 100, 0, 0}, {73 * time.Hour, 100, 0, 0},
		{-time.Hour, 100, 0, 100}, {24*time.Hour + time.Nanosecond, math.MaxInt64, 0, math.MaxInt64 - 53375},
	} {
		created := now.Add(-tc.age)
		if got := unreleasedReward(tc.original, tc.consumed, &created, now); got != tc.want {
			t.Errorf("age=%s locked=%d want=%d", tc.age, got, tc.want)
		}
	}
	if got := unreleasedReward(math.MaxInt64, 0, nil, now); got != 0 {
		t.Fatalf("unknown user age locked=%d", got)
	}
	unknown := time.Unix(0, 0)
	if got := unreleasedReward(100, 0, &unknown, now); got != 0 {
		t.Fatalf("legacy unknown timestamp locked=%d", got)
	}
}
