package legacy

import (
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/billing/pricing"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestImportedPricesRoundInOriginalQuotaUnits(t *testing.T) {
	prices, err := buildPrices(map[string]string{
		"ModelRatio":                   `{"legacy-token":0.5}`,
		"ModelPrice":                   `{"legacy-call":0.000003}`,
		"billing_setting.billing_mode": `{"legacy-expression":"tiered_expr"}`,
		"billing_setting.billing_expr": `{"legacy-expression":"p * 1"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		model string
		usage gateway.Usage
		want  int64
	}{
		{"legacy-token", gateway.Usage{PromptTokens: 1}, 2},
		{"legacy-call", gateway.Usage{}, 4},
		{"legacy-expression", gateway.Usage{PromptTokens: 1}, 2},
	} {
		price := prices[row.model]
		quantum, err := pricing.MoneyQuantum(price)
		if err != nil || quantum != 2 {
			t.Fatalf("%s lost old quota precision: quantum=%d err=%v", row.model, quantum, err)
		}
		actual, err := pricing.Price(row.usage, price, 1)
		if err != nil || int64(actual) != row.want {
			t.Fatalf("%s old-unit rounded cost=%d want=%d err=%v", row.model, actual, row.want, err)
		}
		delete(price.Rules, "money_quantum")
		plain, err := pricing.Price(row.usage, price, 1)
		if err != nil || plain == actual {
			t.Fatalf("%s provenance does not change boundary: native=%d imported=%d err=%v", row.model, plain, actual, err)
		}
	}
}
