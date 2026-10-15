package commerce

import (
	"errors"
	"math"
	"testing"

	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func TestSubscriptionRefundPreservesPaidRatioAfterQuotaChange(t *testing.T) {
	proof := credits.Micro(7_000_000)
	for _, tc := range []struct {
		name                       string
		paid, total, used, current credits.Micro
		origin                     *credits.Micro
		remaining                  credits.Micro
	}{
		{"native", 10_000_000, 10_000_000, 2_000_000, 1_000_000, nil, 7_000_000},
		{"later extra grant", 10_000_000, 20_000_000, 2_000_000, 1_000_000, nil, 7_000_000},
		{"scaled half used", 1_000_000_000, 10_300_000, 5_150_000, 0, nil, 500_000_000},
		{"scaled new usage", 1_000_000_000, 10_300_000, 5_150_000, 1_030_000, nil, 400_000_000},
		{"original proof", 10_000_000, 1_030_000, 0, 103_000, &proof, 2_000_000},
		{"original proof not duplicated", 10_000_000, 1_030_000, 721_000, 103_000, &proof, 2_000_000},
		{"fully spent", 10_000_000, 1_030_000, 1_030_000, 0, nil, 0},
		{"debt", 10_000_000, 1_030_000, 0, 2_060_000, nil, 0},
		{"period only", 10_000_000, 0, 1_000_000, 1_000_000, nil, 8_000_000},
		{"conservative rounding", 10, 3, 1, 0, nil, 6},
		{"large original grant", math.MaxInt64, math.MaxInt64, math.MaxInt64 - 1, 0, nil, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := subscriptionRefundRemaining(tc.paid, tc.total, tc.used, tc.current, tc.origin)
			if err != nil || got != tc.remaining {
				t.Fatalf("remaining=%d err=%v want=%d", got, err, tc.remaining)
			}
		})
	}
}

func TestSubscriptionRefundRejectsInvalidConsumption(t *testing.T) {
	for _, in := range [][4]credits.Micro{{0, 100, 0, 0}, {100, -1, 0, 0}, {100, 100, -1, 0}, {100, 100, 0, -1}} {
		if _, err := subscriptionRefundRemaining(in[0], in[1], in[2], in[3], nil); !errors.Is(err, ErrRefundUnavailable) {
			t.Fatalf("invalid quota accepted: %v", err)
		}
	}
}
