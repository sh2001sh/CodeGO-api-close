package legacy

import (
	"errors"
	"math"
	"testing"

	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func TestFromV2UnitsMatchesDisplayedDollars(t *testing.T) {
	cases := []struct {
		units int64
		want  string // what v2 showed without the "$"
	}{
		{500_000, "1.00"},
		{1, "0.00"},
		{2_500, "0.01"}, // $0.005 rounds to 0.01 at display time only
		{617_280_000, "1234.56"},
		{-250_000, "-0.50"},
	}
	for _, tc := range cases {
		got, err := FromV2Units(tc.units)
		if err != nil {
			t.Fatalf("FromV2Units(%d): %v", tc.units, err)
		}
		if got != credits.Micro(tc.units*2) {
			t.Errorf("FromV2Units(%d) = %d; want exact x2", tc.units, got)
		}
		if got.String() != tc.want {
			t.Errorf("FromV2Units(%d).String() = %q; want %q", tc.units, got.String(), tc.want)
		}
	}
}

func TestFromV2UnitsOverflow(t *testing.T) {
	if _, err := FromV2Units(math.MaxInt64/2 + 1); !errors.Is(err, credits.ErrOverflow) {
		t.Fatalf("expected overflow, got %v", err)
	}
}

func TestOpeningBalanceRejectsNegative(t *testing.T) {
	if _, err := OpeningBalance(-1); !errors.Is(err, ErrNegativeBalance) {
		t.Fatalf("expected ErrNegativeBalance, got %v", err)
	}
}
