package legacy

import (
	"encoding/json"
	"math/big"
	"strings"
	"testing"
)

func TestExactNegotiatedMultiplierKeepsRealDecimalPrices(t *testing.T) {
	for _, source := range []string{"0.08", "1e-8", "1e-14", "1e-20", "1e-13", "1e-63", "0.13114514191981", "0.114514191981", "9223372036854.775807"} {
		value, err := cmExactFactor(source, false)
		if err != nil {
			t.Fatal(source, err)
		}
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		got, valid := new(big.Rat).SetString(string(raw))
		want, _ := new(big.Rat).SetString(source)
		want.Mul(want, big.NewRat(1000000, 1))
		if !valid || got.Cmp(want) != 0 {
			t.Fatalf("price changed source=%s projected=%s", source, raw)
		}
	}
	for _, source := range []string{"", "0", "-0.1", "NaN", "Infinity", "1/10", "\"0.1\"", "1e-257", "1e257", "9223372036854.775808", strings.Repeat("0", 1025)} {
		if _, err := cmExactFactor(source, false); err == nil {
			t.Fatalf("invalid live price accepted: %q", source)
		}
	}
	if value, err := cmExactFactor("0", true); err != nil || value != int64(0) {
		t.Fatal("old cleared notice zero changed", value, err)
	}
	if value, err := cmPublicFactor("0"); err != nil || value != 0 {
		t.Fatal("public free price changed", value, err)
	}
	for _, source := range []string{"", "-0.1", "0.0000001", "NaN"} {
		if _, err := cmPublicFactor(source); err == nil {
			t.Fatal("bad public price accepted", source)
		}
	}
}
