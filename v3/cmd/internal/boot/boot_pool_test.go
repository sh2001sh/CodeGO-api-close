package boot

import (
	"context"
	"strings"
	"testing"
)

func TestPostgresPoolBudget(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		want    int32
		invalid bool
	}{
		{"", 32, false}, {"1", 1, false}, {"12", 12, false},
		{"0", 0, true}, {"-1", 0, true}, {"4.5", 0, true},
		{"2147483648", 0, true}, {"many", 0, true},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			t.Setenv("V3_PG_MAX_CONNS", tc.raw)
			got, err := postgresMaxConns(32)
			if (err != nil) != tc.invalid || got != tc.want {
				t.Fatalf("got (%d, %v), want (%d, invalid=%v)", got, err, tc.want, tc.invalid)
			}
		})
	}
}

func TestOpenRejectsInvalidPoolBudgetBeforeConnecting(t *testing.T) {
	t.Setenv("V3_PG_MAX_CONNS", "0")
	deps, err := Open(context.Background(), 32)
	if deps != nil || err == nil || !strings.Contains(err.Error(), "V3_PG_MAX_CONNS") {
		t.Fatalf("got (%v, %v), expected invalid connection budget", deps, err)
	}
}
