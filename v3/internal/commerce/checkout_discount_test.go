package commerce

import (
	"math"
	"testing"
)

func TestCheckoutDiscountExactDecimalAndWindowBoundaries(t *testing.T) {
	for _, tc := range []struct {
		amount int64
		factor string
		want   int64
	}{
		{10001, "0.80000001", 8001}, {1, "0.01", 1},
		{math.MaxInt64, "0.99999999", 9223371944621055438},
		{101, "0.5", 51},
	} {
		got, err := discountMinor(tc.amount, tc.factor)
		if err != nil || got != tc.want {
			t.Fatalf("%d * %s = %d, err=%v want=%d", tc.amount, tc.factor, got, err, tc.want)
		}
	}
	for _, factor := range []string{"0", "-0.1", "1.1", "not a number"} {
		if _, err := discountMinor(100, factor); err == nil {
			t.Fatalf("accepted %s", factor)
		}
	}
	c := checkoutCampaign{enabled: true, multiplier: "0.8", start: 100, end: 200}
	for _, tc := range []struct {
		now  int64
		want bool
	}{{99, false}, {100, true}, {200, true}, {201, false}} {
		if c.active(tc.now) != tc.want {
			t.Fatalf("window now=%d", tc.now)
		}
	}
	c.enabled = false
	if c.active(150) {
		t.Fatal("disabled campaign active")
	}
	for _, factor := range []string{"0", "1", "-1", "invalid"} {
		c.enabled = true
		c.multiplier = factor
		if c.active(150) {
			t.Fatalf("invalid campaign factor %s", factor)
		}
	}
}
