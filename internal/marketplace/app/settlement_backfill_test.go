package app

import "testing"

func TestSettlementBackfillGross(t *testing.T) {
	for _, test := range []struct {
		name   string
		quota  int64
		source string
		want   int64
	}{
		{name: "wallet", quota: 1234, source: "wallet", want: 1234},
		{name: "subscription rounds down", quota: 124, source: "subscription", want: 12},
		{name: "subscription rounds half up", quota: 125, source: "subscription", want: 13},
		{name: "non positive", quota: 0, source: "wallet", want: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := settlementBackfillGross(test.quota, test.source); got != test.want {
				t.Fatalf("settlementBackfillGross(%d, %q) = %d, want %d", test.quota, test.source, got, test.want)
			}
		})
	}
}
