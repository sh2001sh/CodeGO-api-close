package channelmarket

import (
	"encoding/json"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

func TestEffectiveGroupQuotesMatchSettlementPrecedence(t *testing.T) {
	baseline, _ := json.Marshal([]catalog.Price{{Model: "a", Mode: "per_token", InputPerMTok: 2_000_000, OutputPerMTok: 3_000_000, CacheReadPerMTok: 200_000}, {Model: "b", Mode: "per_request", PerRequest: 5_000_000}})
	group := ChannelView{Models: []string{"a", "b", "missing"}, MultiplierPPM: 500_000, Prices: json.RawMessage(`{}`)}
	prices, err := effectiveModelPrices(group, baseline)
	if err != nil || prices["a"].InputPerMillion != "1" || prices["b"].PerUnit != "2.5" || prices["missing"] != nil {
		t.Fatalf("baseline quotes %#v %v", prices, err)
	}
	group.Prices = json.RawMessage(`{"a":{"billing_mode":"token","input_price_per_million":8,"output_price_per_million":10}}`)
	prices, err = effectiveModelPrices(group, baseline)
	if err != nil || prices["a"].InputPerMillion != "4" || prices["a"].OutputPerMillion != "5" || prices["a"].CacheReadMillion != "0.4" || prices["b"] != nil {
		t.Fatalf("explicit map quotes %#v %v", prices, err)
	}
	baseline, _ = json.Marshal([]catalog.Price{{Model: "a", Mode: "expression", Rules: map[string]any{"expression": "request_count * 2"}}})
	group.Prices = json.RawMessage(`{}`)
	prices, err = effectiveModelPrices(group, baseline)
	if err != nil || prices["a"].Mode != "expression" || prices["a"].Expression == "" {
		t.Fatalf("dynamic quote %#v %v", prices, err)
	}
}

func TestQualityNoSampleDoesNotAdvertiseZeroMeasurements(t *testing.T) {
	quality, err := parseGroupQuality(nil)
	if err != nil || quality != nil {
		t.Fatalf("no snapshot %v %v", quality, err)
	}
	quality, err = parseGroupQuality([]byte(`{"window_hours":24,"request_count":0,"success_rate":null,"cache_hit_rate":null,"average_charge_micro":null,"observing":true,"calculated_at":"2026-10-06T00:00:00Z"}`))
	if err != nil || quality.SuccessRate != nil || quality.CacheHitRate != nil || quality.AverageChargeCredits != nil {
		t.Fatalf("zero samples manufactured data: %+v %v", quality, err)
	}
	quality, err = parseGroupQuality([]byte(`{"window_hours":24,"request_count":2,"success_rate":0,"cache_hit_rate":0.25,"average_charge_micro":250000,"calculated_at":"2026-10-06T00:00:00Z"}`))
	if err != nil || quality.SuccessRate == nil || *quality.SuccessRate != 0 || quality.AverageChargeCredits == nil || *quality.AverageChargeCredits != "0.25" {
		t.Fatalf("measured zero or decimal charge lost: %+v %v", quality, err)
	}
}
