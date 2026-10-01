package catalog

import (
	"encoding/json"
	"testing"
)

// Regression: token/tool prices may exceed float64's exact integer range.
func TestSnapshotPricingIntegerPrecision(t *testing.T) {
	rules, err := unmarshalAnyMap([]byte(`{"tool_prices":{"search":9007199254740993}}`))
	if err != nil {
		t.Fatal(err)
	}
	w := wireSnapshot{Prices: map[string]Price{"model": {Rules: rules}}}
	blob, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeWireSnapshot(blob)
	if err != nil {
		t.Fatal(err)
	}
	amount := decoded.Prices["model"].Rules["tool_prices"].(map[string]any)["search"].(json.Number)
	if amount.String() != "9007199254740993" {
		t.Fatalf("amount rounded: %s", amount)
	}
	if _, err = decodeWireSnapshot(append(blob, []byte(` {}`)...)); err == nil {
		t.Fatal("trailing snapshot data accepted")
	}
}
