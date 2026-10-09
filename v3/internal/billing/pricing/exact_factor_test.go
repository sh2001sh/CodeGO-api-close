package pricing

import (
	"errors"
	"math"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func TestExactMarketplaceFactorPricesWithoutPPMQuantization(t *testing.T) {
	for _, row := range []struct {
		factor string
		price  catalog.Price
		usage  gateway.Usage
		want   credits.Micro
	}{
		{"1000000", catalog.Price{Mode: "per_request", PerRequest: 101}, gateway.Usage{}, 101},
		{"0", catalog.Price{Mode: "per_request", PerRequest: 100000000}, gateway.Usage{}, 0},
		{"0.01", catalog.Price{Mode: "per_request", PerRequest: 100000000}, gateway.Usage{}, 1},
		// A real charge survives with a 1e-20 multiplier; factor rounding to
		// integral PPM would turn this request into a zero charge.
		{"1e-14", catalog.Price{Mode: "per_token", InputPerMTok: 1000000000000000000}, gateway.Usage{PromptTokens: 1000000000000000}, 10000000},
		{"1e-57", expressionPrice("1e63"), gateway.Usage{}, 1},
		{"131145.14191981", catalog.Price{Mode: "per_request", PerRequest: 100000000000000}, gateway.Usage{}, 13114514191981},
	} {
		t.Run(row.factor, func(t *testing.T) {
			amount, err := PriceForRequestExactPPM(row.usage, row.price, row.factor, RequestInput{})
			if err != nil || amount != row.want {
				t.Fatalf("charge=%d err=%v; want %d", amount, err, row.want)
			}
		})
	}
}

func TestExactMarketplaceFactorPreservesToolsFastAndQuantum(t *testing.T) {
	p := catalog.Price{Mode: "per_request", PerRequest: 50000000, Rules: map[string]any{
		FastModeRule: true, "tool_prices": map[string]any{"tool": int64(500)},
	}}
	// Half a model micro plus half a tool micro are combined, then Fast 2x
	// is applied before the one final quantum rounding.
	usage := gateway.Usage{ToolCalls: map[string]int64{"tool": 1}}
	got, err := PriceForRequestExactPPM(usage, p, "0.01", RequestInput{})
	if err != nil || got != 2 {
		t.Fatalf("fast exact/tool=%d/%v", got, err)
	}
	usage.ServiceTier = "default"
	p.Rules["money_quantum"] = 2
	got, err = PriceForRequestExactPPM(usage, p, "0.01", RequestInput{})
	if err != nil || got != 2 {
		t.Fatalf("v2 quantum=%d/%v", got, err)
	}
	delete(p.Rules, "tool_prices")
	got, err = PriceForRequestExactPPM(gateway.Usage{ServiceTier: "default"}, p, "0.01", RequestInput{})
	if err != nil || got != 0 {
		t.Fatalf("below v2 half quantum=%d/%v", got, err)
	}
}

func TestExactMarketplaceFactorFailsClosed(t *testing.T) {
	p := catalog.Price{Mode: "per_request", PerRequest: 10}
	for _, factor := range []string{"", "-1", "NaN", "1/2", "1e999999999"} {
		if _, err := PriceForRequestExactPPM(gateway.Usage{}, p, factor, RequestInput{}); err == nil {
			t.Fatalf("invalid factor %q accepted", factor)
		}
	}
	if _, err := PriceForRequestExactPPM(gateway.Usage{PromptTokens: -1}, p, "0.01", RequestInput{}); err == nil {
		t.Fatal("negative usage accepted")
	}
	p.PerRequest = math.MaxInt64
	if _, err := PriceForRequestExactPPM(gateway.Usage{}, p, "2000000.01", RequestInput{}); !errors.Is(err, credits.ErrOverflow) {
		t.Fatalf("money overflow=%v", err)
	}
}
