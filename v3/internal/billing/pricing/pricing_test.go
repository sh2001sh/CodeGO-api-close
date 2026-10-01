package pricing

import (
	"errors"
	"math"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// gpt4o-like: 2.50 / 10.00 credits per 1M tokens, cached input 1.25.
var tokenPrice = catalog.Price{Mode: "per_token", InputPerMTok: 2_500_000, OutputPerMTok: 10_000_000, CacheReadPerMTok: 1_250_000}

func TestPrice(t *testing.T) {
	cases := []struct {
		name  string
		usage gateway.Usage
		price catalog.Price
		mult  float64
		want  credits.Micro
	}{
		{"one million in and out", gateway.Usage{PromptTokens: 1e6, CompletionTokens: 1e6}, tokenPrice, 1, 12_500_000},
		{"cached part at cache price", gateway.Usage{PromptTokens: 1000, CachedTokens: 400}, tokenPrice, 1, 2_000}, // 600*2.5 + 400*1.25
		{"cached above prompt is clamped", gateway.Usage{PromptTokens: 100, CachedTokens: 500}, tokenPrice, 1, 125},
		{"group multiplier", gateway.Usage{PromptTokens: 1e6}, tokenPrice, 0.8, 2_000_000},
		{"round half up", gateway.Usage{PromptTokens: 1}, catalog.Price{InputPerMTok: 500_000}, 1, 1},          // 0.5 -> 1
		{"below half rounds down", gateway.Usage{PromptTokens: 1}, catalog.Price{InputPerMTok: 499_999}, 1, 0}, // 0.499999 -> 0
		{"per request ignores tokens", gateway.Usage{PromptTokens: 5e6}, catalog.Price{Mode: "per_request", PerRequest: 40_000}, 1.5, 60_000},
		{"zero price", gateway.Usage{PromptTokens: 1e6, CompletionTokens: 1e6}, catalog.Price{}, 1, 0},
		{"zero multiplier", gateway.Usage{PromptTokens: 1e6}, tokenPrice, 0, 0},
	}
	for _, tc := range cases {
		got, err := Price(tc.usage, tc.price, tc.mult)
		if err != nil || got != tc.want {
			t.Errorf("%s: Price = %d, %v; want %d", tc.name, got, err, tc.want)
		}
	}
}

func TestPriceIsExactAtLargeValues(t *testing.T) {
	// 2^53+1 tokens would lose precision in float64; big.Int must not.
	got, err := Price(gateway.Usage{CompletionTokens: 1 << 53}, catalog.Price{OutputPerMTok: 1_000_001}, 1)
	want := credits.Micro(((1<<53)*1_000_001 + 500_000) / 1_000_000)
	if err != nil || got != want {
		t.Fatalf("Price = %d, %v; want %d", got, err, want)
	}
}

func TestPriceRejectsBadInput(t *testing.T) {
	if _, err := Price(gateway.Usage{CompletionTokens: math.MaxInt64}, catalog.Price{OutputPerMTok: math.MaxInt64}, 1); !errors.Is(err, credits.ErrOverflow) {
		t.Fatalf("overflow err = %v", err)
	}
	if _, err := Price(gateway.Usage{}, catalog.Price{Mode: "unknown"}, 1); !errors.Is(err, ErrUnsupportedMode) {
		t.Fatalf("unknown mode err = %v", err)
	}
	for _, m := range []float64{-1, math.NaN(), math.Inf(1)} {
		if _, err := Price(gateway.Usage{}, tokenPrice, m); err == nil {
			t.Fatalf("multiplier %v accepted", m)
		}
	}
	if _, err := Price(gateway.Usage{PromptTokens: -1}, tokenPrice, 1); err == nil {
		t.Fatal("negative tokens accepted")
	}
}

func TestEstimateUsage(t *testing.T) {
	body := []byte(`{"model":"x","max_tokens":256,"messages":[]}`)
	u := EstimateUsage(body, EstimateConfig{})
	if u.CompletionTokens != 256 || u.PromptTokens != int64(len(body)+3)/4 || !u.Estimated {
		t.Fatalf("estimate = %+v", u)
	}
	if u := EstimateUsage([]byte(`{"max_completion_tokens":1000,"max_tokens":5}`), EstimateConfig{}); u.CompletionTokens != 1000 {
		t.Fatalf("max_completion_tokens must win, got %d", u.CompletionTokens)
	}
	if u := EstimateUsage([]byte(`{}`), EstimateConfig{DefaultMaxOutput: 777}); u.CompletionTokens != 777 {
		t.Fatalf("default max output = %d", u.CompletionTokens)
	}
}
