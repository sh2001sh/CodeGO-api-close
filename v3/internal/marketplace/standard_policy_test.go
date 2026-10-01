package marketplace

import (
	"errors"
	"math"
	"testing"

	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func TestStandardPolicyFirstFloorPreservesLegacyValueRules(t *testing.T) {
	p := StandardPolicy{FirstPurchaseMinimumMicro: 10000000, LowRewardThresholdMicro: 10000000}
	for _, tc := range []struct {
		r            Reward
		amount       credits.Micro
		kind, wallet string
	}{
		{Reward{Kind: "credits", Amount: 2000000, LegacyRewardType: "quota", WalletType: "claude"}, 10000000, "credits", "default"},
		{Reward{Kind: "credits", Amount: 15000000, LegacyRewardType: "quota", WalletType: "claude"}, 15000000, "credits", "claude"},
		{Reward{Kind: "credits", Amount: 2000000, LegacyRewardType: "claude_quota", WalletType: "claude"}, 2500000, "credits", "claude"},
		{Reward{Kind: "topup_discount", DiscountRatePPM: 100000}, 10000000, "credits", "default"},
	} {
		r := standardFirstFloor(tc.r, p)
		if r.Amount != tc.amount || r.Kind != tc.kind || r.WalletType != tc.wallet {
			t.Fatalf("first floor %+v => %+v, want amount=%d kind=%s wallet=%s", tc.r, r, tc.amount, tc.kind, tc.wallet)
		}
	}
	for _, tc := range []struct {
		r    Reward
		high bool
	}{
		{Reward{Kind: "credits", Amount: 999999, LegacyRewardType: "claude_quota"}, false},
		{Reward{Kind: "credits", Amount: 1000000, LegacyRewardType: "claude_quota"}, true},
		{Reward{Kind: "credits", Amount: 9999999, LegacyRewardType: "quota"}, false},
		{Reward{Kind: "credits", Amount: 10000000, LegacyRewardType: "quota"}, true},
		{Reward{Kind: "multiplier", Amount: 10000000}, false},
	} {
		if got := standardHighValue(tc.r, p); got != tc.high {
			t.Fatalf("high value %+v: %v, want %v", tc.r, got, tc.high)
		}
	}
	if standardHighValue(Reward{Kind: "credits", Amount: 10000000}, StandardPolicy{}) {
		t.Fatal("disabled threshold must not reset low count")
	}
}

func TestStandardPolicyTierContinuousRoundingAndFailure(t *testing.T) {
	const precision = int64(1 << 53)
	rewards := []Reward{{Kind: "credits", Title: "tier", Weight: 1, Minimum: 1000000, Maximum: 1020000}}
	for _, tc := range []struct {
		draw int64
		want credits.Micro
	}{
		{0, 1000000}, {precision/4 - 1, 1000000}, {precision / 4, 1010000}, {precision*3/4 - 1, 1010000}, {precision * 3 / 4, 1020000}, {precision - 1, 1020000},
	} {
		r, err := standardChooseReward(rewards, func(limit int64) (int64, error) {
			if limit == 1 {
				return 0, nil
			}
			return tc.draw, nil
		})
		if err != nil || r.Amount != tc.want {
			t.Fatalf("draw %d => %d %v, want %d", tc.draw, r.Amount, err, tc.want)
		}
	}
	failure := errors.New("random source failed")
	if _, err := standardChooseReward(rewards, func(int64) (int64, error) { return 0, failure }); !errors.Is(err, failure) {
		t.Fatalf("RNG error: %v", err)
	}
	for _, n := range []int64{-1, precision} {
		if _, err := standardChooseReward(rewards, func(limit int64) (int64, error) {
			if limit == 1 {
				return 0, nil
			}
			return n, nil
		}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("out-of-range RNG %d: %v", n, err)
		}
	}
	overflow := []Reward{{Kind: "credits", Title: "overflow", Weight: 1, Minimum: credits.Micro(math.MaxInt64), Maximum: credits.Micro(math.MaxInt64)}}
	if _, err := standardChooseReward(overflow, func(int64) (int64, error) { return 0, nil }); !errors.Is(err, credits.ErrOverflow) {
		t.Fatalf("rounding overflow: %v", err)
	}
}

func TestStandardPolicyRejectsInvalidFinancialInputs(t *testing.T) {
	for _, p := range []StandardPolicy{
		{SubscriptionProbabilityPPB: -1}, {SubscriptionProbabilityPPB: 1000000001},
		{Enabled: true, SubscriptionProbabilityPPB: 1}, {FirstPurchaseMinimumMicro: -1},
		{PityAfter: -1}, {PityAfter: 1000001}, {PityMinimumMicro: -1}, {LowRewardThresholdMicro: -1},
	} {
		if err := validateStandard(p); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid policy %+v: %v", p, err)
		}
	}
}
