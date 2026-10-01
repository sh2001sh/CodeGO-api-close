package commerce

import (
	"errors"
	"math"
	"testing"
)

func TestCurrencyMinorRoundTripAndPrecisionBoundaries(t *testing.T) {
	for _, tc := range []struct {
		currency string
		amount   int64
		want     string
	}{
		{"jpy", 123, "123"}, {"cny", 123, "1.23"}, {"kwd", 1234, "1.234"},
		{"usdt", 1234567, "1.234567"}, {"usdc", math.MaxInt64, "9223372036854.775807"},
	} {
		t.Run(tc.currency, func(t *testing.T) {
			formatted := formatCurrencyMinor(tc.amount, tc.currency)
			got, err := parseCurrencyMinor(formatted, tc.currency)
			if formatted != tc.want || got != tc.amount || err != nil {
				t.Fatalf("formatted=%s amount=%d err=%v", formatted, got, err)
			}
		})
	}
	for _, raw := range []string{"", "1e6", "1/2", "+1", "-1", " 1", "1.", ".1", "1.2345671", "9223372036854.775808"} {
		if _, err := parseCurrencyMinor(raw, "usdt"); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid amount %q accepted: %v", raw, err)
		}
	}
	if _, err := parseCurrencyMinor("1.1", "jpy"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("fractional JPY accepted: %v", err)
	}
	if got, err := parseCurrencyMinor("1.2300", "cny"); err != nil || got != 123 {
		t.Fatalf("exact provider trailing zeros rejected: %d %v", got, err)
	}
}
