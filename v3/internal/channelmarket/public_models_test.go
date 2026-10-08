package channelmarket

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

func TestPublicModelsQuotesSellerPricesAndExposesOnlyPublicFields(t *testing.T) {
	groups := []ChannelView{{PublicSlug: "seller", Name: "Seller", Models: []string{"gpt"}, MultiplierPPM: 125000,
		Prices: json.RawMessage(`{"gpt":{"input_price_per_million":2,"output_price_per_million":8}}`), Verification: "passed"}}
	metadata := catalog.MetadataSnapshot{Models: []catalog.ModelMetadata{{ID: 17, ModelName: "gpt", Description: "public", VendorID: 2}}, Vendors: []catalog.VendorMetadata{{ID: 2, Name: "OpenAI"}}}
	models, err := buildPublicModels(groups, metadata, map[string]catalog.Price{"gpt": {Mode: "per_token", InputPerMTok: 99000000}})
	if err != nil || len(models) != 1 {
		t.Fatalf("models=%v error=%v", models, err)
	}
	m := models[0]
	p := m.Groups[0].Price
	if m.ModelID == nil || *m.ModelID != 17 || m.Vendor != "OpenAI" || !m.Groups[0].Verified || p.InputPerMillion != "0.25" || p.OutputPerMillion != "1" || p.CacheReadMillion != "0.025" || p.CacheWriteMillion != "0.3125" {
		t.Fatalf("incorrect model quotation: %+v, %+v", m, p)
	}
	if got := publicCreditAmount(math.MaxInt64, 1000000); got != "9223372036854.775807" {
		t.Fatalf("large quotation lost precision: %s", got)
	}
}

func TestPublicModelsMissingSellerEntryDoesNotFallBackToBasePrice(t *testing.T) {
	groups := []ChannelView{{PublicSlug: "seller", Models: []string{"unpriced"}, MultiplierPPM: 1000000,
		Prices: json.RawMessage(`{"different":{"billing_mode":"per_call","price_per_call":1}}`)}}
	models, err := buildPublicModels(groups, catalog.MetadataSnapshot{}, map[string]catalog.Price{"unpriced": {Mode: "per_request", PerRequest: 1000000}})
	if err != nil || len(models) != 1 || models[0].Groups[0].Price != nil || models[0].ModelID != nil {
		t.Fatalf("missing metadata/price must be explicit: %+v, %v", models, err)
	}
	groups[0].Prices = json.RawMessage(`{}`)
	base := map[string]catalog.Price{"unpriced": {Mode: "expression", Rules: map[string]any{"expression": "p * 5 + c * 20", "secret": "private"}}}
	models, err = buildPublicModels(groups, catalog.MetadataSnapshot{}, base)
	if err != nil || models[0].Groups[0].Price.Expression != "p * 5 + c * 20" {
		t.Fatalf("dynamic expression missing: %+v, %v", models, err)
	}
	data, _ := json.Marshal(models)
	var decoded []map[string]any
	if json.Unmarshal(data, &decoded) != nil {
		t.Fatal("invalid JSON")
	}
	price := decoded[0]["groups"].([]any)[0].(map[string]any)["price"].(map[string]any)
	if _, leaked := price["secret"]; leaked {
		t.Fatal("private pricing rules leaked")
	}
}
