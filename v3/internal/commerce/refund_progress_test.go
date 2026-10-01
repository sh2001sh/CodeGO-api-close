package commerce

import (
	"testing"

	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func TestRefundCreditRoundingIsExactAndFinalTotalHasNoDrift(t *testing.T) {
	for _, tc := range []struct {
		grant        credits.Micro
		amount, paid int64
		want         credits.Micro
	}{{1, 1, 2, 1}, {3, 1, 3, 1}, {3, 2, 3, 2}, {3, 3, 3, 3}, {9_007_199_254_740_993, 3, 3, 9_007_199_254_740_993}} {
		if got := refundCredits(tc.grant, tc.amount, tc.paid); got != tc.want {
			t.Fatalf("grant=%d amount=%d paid=%d got=%d want=%d", tc.grant, tc.amount, tc.paid, got, tc.want)
		}
	}
}
