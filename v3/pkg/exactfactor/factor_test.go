package exactfactor

import (
	"math/big"
	"strings"
	"testing"
)

func TestExactFactors(t *testing.T) {
	for _, tc := range []struct{ raw, scaled, multiplier string }{
		{"1000000", "1000000", "1"},
		{"0", "0", "0"},
		{"1e-14", "0.00000000000001", "0.00000000000000000001"},
		{"131145.14191981", "131145.14191981", "0.13114514191981"},
		{"1e-57", "0." + strings.Repeat("0", 56) + "1", "0." + strings.Repeat("0", 62) + "1"},
	} {
		got, err := Resolve(42, tc.raw)
		if err != nil || got != tc.scaled {
			t.Fatalf("Resolve(%q) = %q, %v", tc.raw, got, err)
		}
		got, err = Multiplier(tc.raw)
		if err != nil || got != tc.multiplier {
			t.Fatalf("Multiplier(%q) = %q, %v", tc.raw, got, err)
		}
	}
	if cmp, err := Compare("1e-14", "0"); err != nil || cmp <= 0 {
		t.Fatalf("tiny positive compared to zero = %d, %v", cmp, err)
	}
	if _, ok := Int64("1e-14"); ok {
		t.Fatal("nonintegral PPM was accepted as an integer")
	}
	if got, err := Resolve(1000000, ""); err != nil || got != "1000000" {
		t.Fatalf("integer fallback = %q, %v", got, err)
	}
}

func TestInvalidFactorBoundaries(t *testing.T) {
	for _, raw := range []string{"", "-1", "NaN", "Infinity", "1/3", "1e1000000000", "1e-16384", "1e131072"} {
		if _, err := ParsePPM(raw); err == nil {
			t.Fatalf("accepted invalid factor %q", raw)
		}
	}
	if _, err := ParsePPM("1e-16383"); err != nil {
		t.Fatalf("rejected PostgreSQL numeric scale boundary: %v", err)
	}
	if got := Decimal(big.NewRat(1, 3)); got != "1/3" {
		t.Fatalf("nonterminating rational silently rounded to %q", got)
	}
}
