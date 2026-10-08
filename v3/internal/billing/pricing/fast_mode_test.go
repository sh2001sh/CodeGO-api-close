package pricing

import (
	"errors"
	"math"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func TestFastModeDoublesCacheOutputGroupAndToolCharges(t *testing.T) {
	p := catalog.Price{Mode: "per_token", InputPerMTok: 1_000_000, OutputPerMTok: 2_000_000, CacheReadPerMTok: 250_000, CacheWritePerMTok: 1_250_000,
		Rules: map[string]any{FastModeRule: true, "tool_prices": map[string]any{"web_search": int64(10_000)}}}
	// (70 + 20*0.25 + 10*1.25 + 30*2)*2 group + 10 tool = 305.
	for _, tc := range []struct {
		tier string
		want credits.Micro
	}{
		{"fast", 610}, {"priority", 610}, {"", 610}, {"default", 305}, {"flex", 305}, {"standard", 305},
	} {
		u := gateway.Usage{PromptTokens: 100, CachedTokens: 20, CacheWriteTokens: 10, CompletionTokens: 30, ToolCalls: map[string]int64{"web_search": 1}, ServiceTier: tc.tier}
		got, err := Price(u, p, 2)
		if err != nil || got != tc.want {
			t.Fatalf("tier=%q amount=%d err=%v, want %d", tc.tier, got, err, tc.want)
		}
	}
	delete(p.Rules, FastModeRule)
	got, err := Price(gateway.Usage{PromptTokens: 100, CachedTokens: 20, CacheWriteTokens: 10, CompletionTokens: 30, ToolCalls: map[string]int64{"web_search": 1}, ServiceTier: "fast"}, p, 2)
	if err != nil || got != 305 {
		t.Fatalf("unsolicited upstream fast amount=%d err=%v", got, err)
	}
}

func TestFastModeRoundsOnceAndRejectsOverflow(t *testing.T) {
	p := catalog.Price{Mode: "per_request", PerRequest: 5, Rules: map[string]any{FastModeRule: true, "money_quantum": 2}}
	got, err := Price(gateway.Usage{}, p, 1)
	if err != nil || got != 10 {
		t.Fatalf("amount=%d err=%v; want 10", got, err)
	}
	p.PerRequest = math.MaxInt64/2 + 1
	if _, err = Price(gateway.Usage{}, p, 1); !errors.Is(err, credits.ErrOverflow) {
		t.Fatalf("overflow=%v", err)
	}
}
