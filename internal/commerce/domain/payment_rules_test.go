package domain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyDiscountRateToMoneyWithCap(t *testing.T) {
	for _, test := range []struct {
		name   string
		amount float64
		want   float64
	}{
		{name: "below cap", amount: 999, want: 899.1},
		{name: "at cap", amount: 1000, want: 900},
		{name: "just above cap", amount: 1001, want: 901},
		{name: "well above cap", amount: 3000, want: 2900},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.InDelta(t, test.want, ApplyDiscountRateToMoneyWithCap(test.amount, 0.10, 100), 0.001)
		})
	}
}

func TestCappedDiscountRateWithoutCapKeepsOriginalRate(t *testing.T) {
	require.InDelta(t, 0.10, CappedDiscountRate(3000, 0.10, 0), 0.000001)
	require.Zero(t, CappedDiscountRate(0, 0.10, 100))
}
