package pricing

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func expressionPrice(source string) catalog.Price {
	return catalog.Price{Mode: "expression", Rules: map[string]any{"expression": source}}
}

func TestExpressionPricingV2Vectors(t *testing.T) {
	// These expressions come from v2 billingexpr and billing_setting, using
	// the same price coefficients with exact micro-credit expected results.
	tests := []struct {
		source string
		usage  gateway.Usage
		want   credits.Micro
	}{
		{`tier("base", p * 2.5 + c * 15 + cr * 0.25)`, gateway.Usage{PromptTokens: 1000, CompletionTokens: 200, CachedTokens: 400}, 4600},
		{`p * 2.5 + c * 15`, gateway.Usage{PromptTokens: 1000, CompletionTokens: 200, CachedTokens: 400}, 5500},
		{`len <= 200000 ? tier("standard", p * 3 + c * 15 + cr * 0.3) : tier("long", p * 6 + c * 22.5 + cr * 0.6)`, gateway.Usage{PromptTokens: 300000, CachedTokens: 250000, CompletionTokens: 1000}, 472500},
		{`len <= 200000 ? p * 3 : p * 6`, gateway.Usage{PromptTokens: 200000}, 600000},
		{`len <= 200000 ? p * 3 : p * 6`, gateway.Usage{PromptTokens: 200001}, 1200006},
		{`p < 32000 && c < 200 ? tier("short", p * 2 + c * 8) : p < 32000 && c >= 200 ? tier("output", p * 3 + c * 14) : tier("input", p * 4 + c * 16)`, gateway.Usage{PromptTokens: 1000, CompletionTokens: 200}, 5800},
		{`v1:tier("default", p * 1.5 + c * 7.5 + cr * 0.15 + cc * 2 + cc1h * 3)`, gateway.Usage{PromptTokens: 1000, CachedTokens: 200, CacheWriteTokens: 100, CacheWrite1hTokens: 50, CompletionTokens: 100}, 2105},
		{`p * 2 + c * 10 + img * 5 + ai * 50 + img_o * 20 + ao * 100`, gateway.Usage{PromptTokens: 1000, CompletionTokens: 500, ImageInputTokens: 100, AudioInputTokens: 200, ImageOutputTokens: 100, AudioOutputTokens: 50}, 22400},
		{`p * 0.5`, gateway.Usage{PromptTokens: 1}, 1},
		{`p * 0.499999`, gateway.Usage{PromptTokens: 1}, 0},
		{`p * 1.000001`, gateway.Usage{PromptTokens: 1 << 53}, 9007208261940247},
		{`true ? p * 3 : 1 / 0`, gateway.Usage{PromptTokens: 2}, 6},
		{`false && 1 / 0 > 0 ? 100 : 2`, gateway.Usage{}, 2},
		{`max(2, min(5, 4)) + abs(-1.5) + ceil(0.1) + floor(-0.1)`, gateway.Usage{}, 6},
	}
	for _, tc := range tests {
		got, err := Price(tc.usage, expressionPrice(tc.source), 1)
		if err != nil || got != tc.want {
			t.Errorf("%s: got %d, %v; want %d", tc.source, got, err, tc.want)
		}
	}
}

func TestExpressionRequestInputsAndFrozenTime(t *testing.T) {
	now := time.Date(2026, 9, 30, 0, 17, 0, 0, time.UTC)
	request := RequestInput{Body: []byte(`{"fast":true,"seed":9007199254740993}`), Headers: map[string]string{"Anthropic-Beta": " fast-mode "}, Now: now}
	source := `param("fast") == true && param("seed") == 9007199254740993 ? p * 2 : p * 1`
	got, err := PriceForRequest(gateway.Usage{PromptTokens: 3}, expressionPrice(source), 1, request)
	if err != nil || got != 6 {
		t.Fatalf("request values: %d, %v", got, err)
	}
	source = `tier("base", p * 2)|||when(header("anthropic-beta") has "fast-mode") * 6|||when(hour("Asia/Shanghai") == 8 && minute("UTC") == 17 && weekday("UTC") == 3 && month("UTC") == 9 && day("UTC") == 30) * 0.5`
	got, err = PriceForRequest(gateway.Usage{PromptTokens: 3}, expressionPrice(source), 1, request)
	if err != nil || got != 18 {
		t.Fatalf("rules/time: %d, %v", got, err)
	}
	request.Headers = nil
	got, err = PriceForRequest(gateway.Usage{PromptTokens: 3}, expressionPrice(source), 1, request)
	if err != nil || got != 3 {
		t.Fatalf("unmatched rule: %d, %v", got, err)
	}
}

func TestToolsAndCacheWriteRoundOnlyOnce(t *testing.T) {
	p := catalog.Price{InputPerMTok: 500000, CacheWritePerMTok: 2000000, Rules: map[string]any{"tool_prices": map[string]any{"web_search": "500"}}}
	got, err := Price(gateway.Usage{PromptTokens: 1, ToolCalls: map[string]int64{"web_search": 1}}, p, 1)
	if err != nil || got != 1 {
		t.Fatalf("combined half charges should round once: %d, %v", got, err)
	}
	got, err = Price(gateway.Usage{PromptTokens: 10, ToolCalls: map[string]int64{"web_search": 10}}, p, 0.5)
	if err != nil || got != 8 {
		t.Fatalf("tool surcharge stays absolute after model discount: %d, %v", got, err)
	}
	got, err = Price(gateway.Usage{PromptTokens: 1000, CacheWriteTokens: 100, CacheWrite1hTokens: 50}, p, 1)
	if err != nil || got != 725 {
		t.Fatalf("cache writes: %d, %v", got, err)
	}
	p.Model = "gpt-4o-mini"
	p.Rules = map[string]any{"tool_prices": map[string]any{"web_search_preview:gpt-*": int64(30_000_000), "web_search_preview:gpt-4o*": int64(40_000_000)}}
	got, err = Price(gateway.Usage{ToolCalls: map[string]int64{"web_search_preview": 1}}, p, 1)
	if err != nil || got != 40000 {
		t.Fatalf("longest model override: %d, %v", got, err)
	}
	p.Rules = nil
	got, err = Price(gateway.Usage{ToolCalls: map[string]int64{"web_search_preview": 1, "file_search": 2}}, p, 1)
	if err != nil || got != 30000 {
		t.Fatalf("v2 defaults: %d, %v", got, err)
	}
}

func TestExpressionRejectsBadPricesAndInputs(t *testing.T) {
	for _, source := range []string{
		``, `p +`, `unknown * 1`, `run("anything")`, `1 / 0`, `-1`, `true`,
		`v2:p * 1`, `param("array") * 1`, `hour("UTC") * 1`,
		`p ||| when(true) * -1`, `p |||garbage`, `1e999999`,
		strings.Repeat("(", 65) + "1" + strings.Repeat(")", 65),
	} {
		if _, err := Price(gateway.Usage{}, expressionPrice(source), 1); err == nil {
			t.Errorf("accepted invalid expression %q", source)
		}
	}
	if _, err := PriceForRequest(gateway.Usage{}, expressionPrice(`hour("Invalid/Zone")`), 1, RequestInput{Now: time.Now()}); err == nil {
		t.Fatal("invalid timezone accepted")
	}
	for _, u := range []gateway.Usage{{CacheWriteTokens: -1}, {AudioOutputTokens: -1}, {ToolCalls: map[string]int64{"x": -1}}, {CacheWriteTokens: math.MaxInt64, CacheWrite1hTokens: 1}} {
		if _, err := Price(u, catalog.Price{}, 1); err == nil {
			t.Errorf("accepted invalid usage %+v", u)
		}
	}
	for _, p := range []catalog.Price{{InputPerMTok: -1}, {Mode: "per_request", PerRequest: -1}} {
		if _, err := Price(gateway.Usage{}, p, 1); err == nil {
			t.Errorf("accepted negative catalog price %+v", p)
		}
	}
	for _, toolPrice := range []any{nil, -1, 0.5, "bad", float64(1 << 53), "9223372036854775808"} {
		p := catalog.Price{Rules: map[string]any{"tool_prices": map[string]any{"x": toolPrice}}}
		if _, err := Price(gateway.Usage{ToolCalls: map[string]int64{"x": 1}}, p, 1); err == nil {
			t.Errorf("accepted invalid tool price %v", toolPrice)
		}
	}
	if _, err := Price(gateway.Usage{}, catalog.Price{Rules: map[string]any{"tool_prices": "bad"}}, 1); err == nil {
		t.Fatal("malformed tool price table accepted")
	}
	if _, err := Price(gateway.Usage{ToolCalls: map[string]int64{"x": 1}}, catalog.Price{}, 1); err == nil {
		t.Fatal("missing tool price accepted")
	}
	if _, err := Price(gateway.Usage{}, catalog.Price{}, math.MaxFloat64); err == nil {
		t.Fatal("overflowing multiplier accepted")
	}
}

func TestExpressionCatalogValidation(t *testing.T) {
	for _, source := range []string{`tier("base",p * 2 + c * 10)`, `param("fast") == true ? p * 2 : p`, `p |||when(header("x") has "fast") * 6`} {
		if err := Validate(expressionPrice(source)); err != nil {
			t.Errorf("valid expression %s: %v", source, err)
		}
	}
	for _, source := range []string{``, `p+`, `unknown`, `p|||when(true)`, `v2:p`} {
		if err := Validate(expressionPrice(source)); err == nil {
			t.Errorf("invalid expression accepted: %s", source)
		}
	}
}

func BenchmarkExpressionPricing(b *testing.B) {
	p := expressionPrice(`len <= 200000 ? tier("standard",p * 3 + c * 15 + cr * 0.3 + cc * 3.75) : tier("long",p * 6 + c * 22.5 + cr * 0.6 + cc * 7.5)`)
	u := gateway.Usage{PromptTokens: 300000, CachedTokens: 250000, CompletionTokens: 1000}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Price(u, p, 1); err != nil {
			b.Fatal(err)
		}
	}
}
