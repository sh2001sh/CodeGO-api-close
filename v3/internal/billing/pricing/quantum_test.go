package pricing

import (
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestMigratedQuotaRoundingUsesTwoMicroBoundary(t *testing.T) {
	for _, row := range []struct{ amount, want int64 }{{1, 2}, {3, 4}, {5, 6}, {6, 6}, {0, 0}} {
		p := catalog.Price{Mode: "per_request", PerRequest: row.amount, Rules: map[string]any{"money_quantum": 2}}
		got, err := Price(gateway.Usage{}, p, 1)
		if err != nil || int64(got) != row.want {
			t.Fatalf("migrated %d=%d/%v want%d", row.amount, got, err, row.want)
		}
		delete(p.Rules, "money_quantum")
		got, err = Price(gateway.Usage{}, p, 1)
		if err != nil || int64(got) != row.amount {
			t.Fatalf("native %d=%d/%v", row.amount, got, err)
		}
	}
	for _, invalid := range []any{0, 3, "2.5", "invalid", nil} {
		if err := Validate(catalog.Price{Mode: "per_request", PerRequest: 2, Rules: map[string]any{"money_quantum": invalid}}); err == nil {
			t.Fatalf("invalid quantum%v accepted", invalid)
		}
	}
}
