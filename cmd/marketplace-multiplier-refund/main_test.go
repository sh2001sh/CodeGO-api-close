package main

import "testing"

func TestCalculateRefund(t *testing.T) {
	tests := []struct {
		name       string
		amount     int64
		multiplier float64
		want       int64
	}{
		{name: "multiplier discount", amount: 2_508, multiplier: 0.17, want: 2_082},
		{name: "rounds corrected charge", amount: 5, multiplier: 0.5, want: 2},
		{name: "zero amount", amount: 0, multiplier: 0.17, want: 0},
		{name: "full rate", amount: 100, multiplier: 1, want: 0},
		{name: "invalid multiplier", amount: 100, multiplier: 0, want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := calculateRefund(test.amount, test.multiplier); got != test.want {
				t.Fatalf("calculateRefund(%d, %f) = %d, want %d", test.amount, test.multiplier, got, test.want)
			}
		})
	}
}
