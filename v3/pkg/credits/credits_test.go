package credits

import (
	"errors"
	"math"
	"testing"
)

func TestFromCredits(t *testing.T) {
	got, err := FromCredits(12)
	if err != nil || got != 12_000_000 {
		t.Fatalf("FromCredits(12) = %d, %v; want 12000000, nil", got, err)
	}
	if _, err := FromCredits(math.MaxInt64/PerCredit + 1); !errors.Is(err, ErrOverflow) {
		t.Fatalf("expected overflow, got %v", err)
	}
}

func TestAddOverflow(t *testing.T) {
	if _, err := Micro(math.MaxInt64).Add(1); !errors.Is(err, ErrOverflow) {
		t.Fatalf("expected overflow, got %v", err)
	}
	if _, err := Micro(math.MinInt64).Add(-1); !errors.Is(err, ErrOverflow) {
		t.Fatalf("expected overflow, got %v", err)
	}
	if got, err := Micro(5).Add(-7); err != nil || got != -2 {
		t.Fatalf("5 + -7 = %d, %v", got, err)
	}
}

func TestString(t *testing.T) {
	cases := []struct {
		in   Micro
		want string
	}{
		{0, "0.00"},
		{1_234_560_000, "1234.56"},
		{1_000_000, "1.00"},
		{5_000, "0.01"}, // half rounds up
		{4_999, "0.00"}, // below half rounds down
		{-2_500_000, "-2.50"},
		{-4_999, "0.00"}, // no negative zero
		{Micro(math.MinInt64), "-9223372036854.78"},
	}
	for _, tc := range cases {
		if got := tc.in.String(); got != tc.want {
			t.Errorf("Micro(%d).String() = %q; want %q", int64(tc.in), got, tc.want)
		}
	}
}
