package commerce

import (
	"math"
	"testing"

	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func TestUserRefundMoneyRoundsHalfUpWithoutIntegerOverflow(t *testing.T) {
	for _, tc := range []struct {
		paid             int64
		remaining, total credits.Micro
		want             int64
	}{
		{1000, 3, 10, 300}, {1000, 1, 3, 333}, {1, 1, 2, 1}, {0, 1, 2, 0}, {1000, 0, 10, 0},
		{math.MaxInt64, 1, 2, math.MaxInt64/2 + 1}, {math.MaxInt64, math.MaxInt64, math.MaxInt64, math.MaxInt64},
	} {
		if got := proportionalMinor(tc.paid, tc.remaining, tc.total); got != tc.want {
			t.Errorf("%d * %d/%d = %d; want %d", tc.paid, tc.remaining, tc.total, got, tc.want)
		}
	}
	item := refundQuote(Order{Provider: "epay", Currency: "cny", State: "paid", AmountMinor: 25, Kind: "topup", Credits: 1}, 1, 1, "")
	if item.FeeAmount != "0.01" || item.RefundAmount != "0.24" || item.RefundAmountMinor != 24 {
		t.Fatalf("fee tie: %+v", item)
	}
}
