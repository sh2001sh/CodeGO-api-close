package legacy

import (
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/billing/pricing"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestImportedMediaPricesUseActualOutputDimensions(t *testing.T) {
	prices, err := buildPrices(map[string]string{"ModelPrice": `{"dall-e-3":0.04,"veo-3.1-generate-preview":0.40,"suno-v3":0.10}`})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		model  string
		usage  gateway.Usage
		amount int64
	}{
		{"dall-e-3", gateway.Usage{ImageCount: 3}, 120000},
		{"veo-3.1-generate-preview", gateway.Usage{VideoDurationMicros: 5_500_000}, 2200000},
		{"suno-v3", gateway.Usage{}, 100000},
	} {
		actual, priceErr := pricing.Price(tc.usage, prices[tc.model], 1)
		if priceErr != nil || int64(actual) != tc.amount {
			t.Fatalf("%s actual=%d want=%d err=%v", tc.model, actual, tc.amount, priceErr)
		}
	}
	if _, err = pricing.Price(gateway.Usage{}, prices["dall-e-3"], 1); err == nil {
		t.Fatal("missing image dimension charged a call silently")
	}
}
