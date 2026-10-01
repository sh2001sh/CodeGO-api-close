package catalog

import "testing"

func TestMarketPricesExactMicroPrecisionAndDefaults(t *testing.T) {
	prices, err := ParseMarketPrices([]byte(`{"model":{"input_price_per_million":9007199254.740993,"output_price_per_million":"0.0000005"}}`))
	if err != nil {
		t.Fatal(err)
	}
	p := prices["model"]
	if p.Model != "model" || p.InputPerMTok != 9007199254740993 || p.OutputPerMTok != 1 || p.CacheReadPerMTok != 900719925474099 || p.CacheWritePerMTok != 11258999068426241 {
		t.Fatalf("market price rounded incorrectly: %+v", p)
	}
	for _, raw := range []string{`null`, `[]`, `{"x":{"billing_mode":"per_call","price_per_call":0}}`, `{"x":{"input_price_per_million":-1}}`, `{"x":{"input_price_per_million":9223372036855}}`, `{"x":{"billing_mode":"unsupported"}}`} {
		if _, err := ParseMarketPrices([]byte(raw)); err == nil {
			t.Fatalf("invalid market price accepted: %s", raw)
		}
	}
}

func TestMarketPricesPreserveImportedMoneyQuantumAndRejectInvalid(t *testing.T) {
	for _, quantum := range []string{"0", "1", "2"} {
		prices, err := ParseMarketPrices([]byte(`{"model":{"input_price_per_million":0.1,"money_quantum":` + quantum + `}}`))
		if err != nil {
			t.Fatal(err)
		}
		if value, ok := prices["model"].Rules["money_quantum"]; ok != (quantum == "2") || (ok && value != int64(2)) {
			t.Fatalf("source quantum provenance changed: quantum=%s price=%+v", quantum, prices["model"])
		}
	}
	for _, quantum := range []string{"-1", "3", "1.5", `"2"`, "9223372036854775808"} {
		if _, err := ParseMarketPrices([]byte(`{"model":{"input_price_per_million":0.1,"money_quantum":` + quantum + `}}`)); err == nil {
			t.Fatalf("invalid source rounding quantum accepted: %s", quantum)
		}
	}
}
