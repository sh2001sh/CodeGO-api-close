package legacy

import (
	"encoding/json"
	"testing"
)

func TestHistoricalSettlementSubscriptionFactorRecognizesOnlyExactV2Operation(t *testing.T) {
	for _, tc := range []struct {
		wallet, subscription string
		want                 int64
		bad                  bool
	}{
		{"0.17", "1.7000000000000002", 1700000, false},
		{"0.09", "0.8999999999999999", 900000, false},
		{"0.085", "0.8500000000000001", 850000, false},
		{"0.18", "1.7999999999999998", 1800000, false},
		{"0.07", "0.7000000000000001", 700000, false},
		{"0.17", "1.7000000000000002000", 1700000, false},
		{"0.17", "0", 0, false},
		{"0.17", "0.3", 300000, false},
		{"0.17", "1.7000000000000003", 0, true},
		{"0.17", "0.8999999999999999", 0, true},
		{"0.17000000000000002", "1.7000000000000002", 0, true},
		{"0.0000001", "0.000001", 1, false},
		{"0.17", "-1.7000000000000002", 0, true},
		{"0.17", "NaN", 0, true},
		{"9223372036854.775808", "1.7000000000000002", 0, true},
	} {
		t.Run(tc.wallet+"/"+tc.subscription, func(t *testing.T) {
			row := cmRow{"multiplier": json.RawMessage(tc.wallet), "subscription_multiplier": json.RawMessage(tc.subscription)}
			if tc.subscription == "NaN" {
				row["subscription_multiplier"] = json.RawMessage(`"NaN"`)
			}
			before, _ := json.Marshal(row)
			b := cmBuild()
			b.settlementFactors(row)
			after, _ := json.Marshal(row)
			if string(before) != string(after) {
				t.Fatal("source factor changed")
			}
			if (b.err != nil) != tc.bad || (!tc.bad && b.values["subscription_multiplier_ppm"] != tc.want) {
				t.Fatalf("factor projection=%v err=%v", b.values, b.err)
			}
			if _, err := cmFactor(tc.subscription, true); err == nil && tc.subscription == "1.7000000000000002" {
				t.Fatal("global exact factor unexpectedly accepts a residue")
			}
		})
	}
}

func TestHistoricalSettlementWalletFactorPreservesExactFractionalPPM(t *testing.T) {
	for _, tc := range []struct {
		input, want string
		bad         bool
	}{
		{"0.13114514191981", "131145.14191981", false},
		{"0.0000001", "0.1", false},
		{"1.3114514191981e-1", "131145.14191981", false},
		{"9223372036854.7758061", "9223372036854775806.1", false},
		{"9223372036854.7758071", "", true},
		{"-0.13114514191981", "", true},
		{"0", "", true}, {"", "", true}, {"null", "", true},
		{"NaN", "", true}, {"Infinity", "", true}, {"1/3", "", true},
	} {
		t.Run(tc.input, func(t *testing.T) {
			value, err := historicalSettlementFactor(tc.input)
			if (err != nil) != tc.bad || (!tc.bad && value != json.Number(tc.want)) {
				t.Fatalf("historical factor=%v err=%v", value, err)
			}
		})
	}
}
