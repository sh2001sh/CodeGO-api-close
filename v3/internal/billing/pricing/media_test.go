package pricing

import (
	"errors"
	"math"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func mediaPrice(unit string, amount int64) catalog.Price {
	return catalog.Price{Mode: "per_request", PerRequest: amount, Rules: map[string]any{"billing_unit": unit}}
}

// Regression: an image's actual count and seconds/characters are accounting
// dimensions, never fabricated prompt or completion token counts.
func TestMediaUnitCharges(t *testing.T) {
	tests := []struct {
		name  string
		usage gateway.Usage
		price catalog.Price
		group float64
		want  credits.Micro
	}{
		{"actual images", gateway.Usage{ImageCount: 3}, mediaPrice("image", 40_000), 1, 120_000},
		{"partial upstream image result", gateway.Usage{ImageCount: 2}, mediaPrice("image", 40_000), 1, 80_000},
		{"audio seconds", gateway.Usage{AudioDurationMicros: 1_250_000}, mediaPrice("audio_second", 250), 1, 313},
		{"video seconds", gateway.Usage{VideoDurationMicros: 5_750_000}, mediaPrice("video_second", 400_000), 0.5, 1_150_000},
		{"TTS characters", gateway.Usage{AudioCharacters: 7}, mediaPrice("audio_character", 15), 1, 105},
		{"per call task ignores all units", gateway.Usage{ImageCount: 4, VideoDurationMicros: 8_000_000}, catalog.Price{Mode: "per_request", PerRequest: 100_000}, 1, 100_000},
		{"upstream token usage unchanged", gateway.Usage{PromptTokens: 100, CompletionTokens: 20, ImageCount: 4, AudioDurationMicros: 1_000_000}, catalog.Price{Mode: "per_token", InputPerMTok: 2_000_000, OutputPerMTok: 4_000_000}, 1, 280},
		{"sub-microcharge rounds once", gateway.Usage{AudioDurationMicros: 250_000}, mediaPrice("audio_second", 1), 2, 1},
		{"zero published price still needs usage", gateway.Usage{ImageCount: 1}, mediaPrice("image", 0), 1, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Price(test.usage, test.price, test.group)
			if err != nil || got != test.want {
				t.Fatalf("Price = %d, %v; want %d", got, err, test.want)
			}
		})
	}
	p := mediaPrice("audio_second", 250)
	p.Rules["unit_rounding"] = "ceil_second"
	got, err := Price(gateway.Usage{AudioDurationMicros: 60_000_001}, p, 1)
	if err != nil || got != 15_250 {
		t.Fatalf("ceil_second Price = %d, %v; want 15250", got, err)
	}
	got, err = Price(gateway.Usage{AudioDurationMicros: math.MaxInt64}, mediaPrice("audio_second", 1), 1)
	if err != nil || got != 9_223_372_036_855 {
		t.Fatalf("MaxInt64 duration Price = %d, %v", got, err)
	}
}

func TestMediaToolsAndMultiplierRoundOnce(t *testing.T) {
	p := mediaPrice("audio_second", 1)
	p.Rules["tool_prices"] = map[string]any{"custom": int64(250)}
	usage := gateway.Usage{AudioDurationMicros: 250_000, ToolCalls: map[string]int64{"custom": 1}}
	got, err := Price(usage, p, 1)
	if err != nil || got != 1 {
		t.Fatalf("joint half-up price = %d, %v; want 1", got, err)
	}
	got, err = Price(usage, p, 0)
	if err != nil || got != 0 {
		t.Fatalf("group zero with absolute fractional tool charge = %d, %v", got, err)
	}
	p.Rules["tool_prices"] = map[string]any{"custom": int64(1000)}
	got, err = Price(usage, p, 0)
	if err != nil || got != 1 {
		t.Fatalf("tool charge bypasses group = %d, %v; want 1", got, err)
	}
}

func TestMediaInvalidPricesAndUnits(t *testing.T) {
	for _, p := range []catalog.Price{
		mediaPrice("", 1), mediaPrice("unknown", 1), mediaPrice("image", -1),
		{Mode: "per_token", Rules: map[string]any{"billing_unit": "image"}},
		{Mode: "per_request", Rules: map[string]any{"billing_unit": 1}},
		{Mode: "per_request", Rules: map[string]any{"unit_rounding": "ceil_second"}},
		{Mode: "per_request", Rules: map[string]any{"billing_unit": "image", "unit_rounding": "ceil_second"}},
		{Mode: "per_request", Rules: map[string]any{"billing_unit": "audio_second", "unit_rounding": "unknown"}},
	} {
		if err := Validate(p); err == nil {
			t.Fatalf("accepted invalid media price %#v", p)
		}
		if _, err := Price(gateway.Usage{ImageCount: 1}, p, 1); err == nil {
			t.Fatalf("computed invalid media price %#v", p)
		}
	}
	for _, unit := range []string{"image", "audio_second", "audio_character", "video_second"} {
		if _, err := Price(gateway.Usage{}, mediaPrice(unit, 1), 1); err == nil {
			t.Fatalf("accepted missing %s usage", unit)
		}
	}
	for _, usage := range []gateway.Usage{{ImageCount: -1}, {AudioDurationMicros: -1}, {AudioCharacters: -1}, {VideoDurationMicros: -1}} {
		if _, err := Price(usage, catalog.Price{Mode: "per_request"}, 1); err == nil {
			t.Fatalf("accepted negative usage %#v", usage)
		}
	}
	if _, err := Price(gateway.Usage{ImageCount: 2}, mediaPrice("image", math.MaxInt64), 1); !errors.Is(err, credits.ErrOverflow) {
		t.Fatalf("overflow = %v", err)
	}
	got, err := Price(gateway.Usage{ImageCount: 1}, mediaPrice("image", 9_007_199_254_740_993), 1)
	if err != nil || got != 9_007_199_254_740_993 {
		t.Fatalf("integer precision = %d, %v", got, err)
	}
}

func TestWithMediaUnits(t *testing.T) {
	got, err := WithMediaUnits(gateway.Usage{PromptTokens: 17}, "video_second", "5.000001")
	if err != nil || got.VideoDurationMicros != 5_000_001 || got.PromptTokens != 17 {
		t.Fatalf("typed duration = %#v, %v", got, err)
	}
	got, err = WithMediaUnits(gateway.Usage{ImageCount: 2}, "image", "NaN")
	if err != nil || got.ImageCount != 2 {
		t.Fatalf("authoritative usage = %#v, %v", got, err)
	}
	for _, test := range []struct{ unit, units string }{
		{"image", "0"}, {"image", "-1"}, {"image", "1.5"}, {"audio_character", "2.5"},
		{"audio_second", "0.0000001"}, {"video_second", "NaN"}, {"unknown", "1"}, {"image", "9223372036854775808"},
	} {
		if _, err := WithMediaUnits(gateway.Usage{}, test.unit, test.units); err == nil {
			t.Fatalf("accepted invalid units %#v", test)
		}
	}
}
