package incentives

import (
	"math"
	"testing"
)

func TestReferralAwardContributionCapRoundingAndOverflow(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		revenue, cost, cap, want int64
	}{
		{"ordinary", 100000000, 92700000, 2000000, 1000000},
		{"thin profit", 100000000, 99800000, 2000000, 40000},
		{"loss", 100000000, 101000000, 2000000, 0},
		{"frozen cap", 100000000, 0, 12345, 12345},
		{"floor", 99, 0, 2000000, 0},
		{"int64 intermediates", math.MaxInt64, 0, math.MaxInt64, 92233720368547758},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := referralAward(tc.revenue, tc.cost, 10000, 200000, tc.cap)
			if err != nil || got != tc.want {
				t.Fatalf("got=%d want=%d err=%v", got, tc.want, err)
			}
		})
	}
	if _, err := referralAward(-1, 0, 10000, 200000, 100); err == nil {
		t.Fatal("negative revenue accepted")
	}
	if got, err := referralAncillaryCost(1, 1); err != nil || got != 1 {
		t.Fatalf("conservative ancillary rounding=%d err=%v", got, err)
	}
}
