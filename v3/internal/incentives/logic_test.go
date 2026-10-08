package incentives

import (
	"errors"
	"testing"

	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func TestExactDecimalRewardsAndOverflow(t *testing.T) {
	for _, c := range []struct {
		base, multiplier, jackpot string
		want                      credits.Micro
	}{
		{"0.25", "1.1", "0", 280000}, {"25", "1.3", "12.5", 45000000}, {"0.004999999999999999999", "1", "0", 0}, {"0.005", "1", "0", 10000},
	} {
		got, err := rewardMicro(c.base, c.multiplier, c.jackpot)
		if err != nil || got != c.want {
			t.Fatalf("%+v: got %d: %v", c, got, err)
		}
	}
	if _, err := rewardMicro("9223372036854775808", "1", "0"); !errors.Is(err, credits.ErrOverflow) {
		t.Fatalf("overflow accepted: %v", err)
	}
	if _, err := rewardMicro("NaN", "1", "0"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad decimal accepted: %v", err)
	}
	if _, err := rewardMicro("1e1000000000", "1", "0"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unbounded exponent accepted: %v", err)
	}
}
func TestSettingsRejectCombinedPayoutOverflow(t *testing.T) {
	c := defaultSettings()
	c.Base4 = "9000000000000"
	c.Ultra = "9000000000000"
	if err := c.validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("overflowing payout configured: %v", err)
	}
}
