package legacy

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/billing/pricing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestMarketImportedPricesPreserveExactFieldsAndQuotaQuantum(t *testing.T) {
	data := cmUnitData(t)
	source := json.RawMessage(`{"chat-model":{"billing_mode":"per_call","price_per_call":0.000003,"money_quantum":1},"token-model":{"input_price_per_million":1,"output_price_per_million":2.0000000000000000000000001,"cache_read_price_per_million":0.1234567890123456789,"cache_write_price_per_million":9007199254.740993}}`)
	data.rows["channels"][0]["model_prices"] = source
	before := append([]byte(nil), source...)
	data.prepare("")
	if len(data.issues) != 0 {
		t.Fatalf("prepare issues=%+v", data.issues)
	}
	var projected json.RawMessage
	for _, record := range data.records {
		if record.table == "v3_channelmarket.groups" {
			projected = record.values["model_prices"].(json.RawMessage)
		}
	}
	var original, mapped map[string]map[string]json.RawMessage
	if err := json.Unmarshal(source, &original); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(projected, &mapped); err != nil {
		t.Fatal(err)
	}
	for model, fields := range original {
		if string(mapped[model]["money_quantum"]) != "2" {
			t.Fatalf("%s imported quantum=%s, want 2", model, mapped[model]["money_quantum"])
		}
		for key, value := range fields {
			if key != "money_quantum" && !bytes.Equal(value, mapped[model][key]) {
				t.Fatalf("%s.%s numeric precision changed", model, key)
			}
		}
	}
	if !bytes.Equal(before, data.rows["channels"][0]["model_prices"]) {
		t.Fatal("source price document was mutated")
	}
	prices, err := catalog.ParseMarketPrices(projected)
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range []string{"chat-model", "token-model"} {
		amount, err := pricing.Price(gateway.Usage{PromptTokens: 3}, prices[model], 1)
		if err != nil || amount != 4 {
			t.Fatalf("%s old quota half-up x2 charge=%d/%v, want 4", model, amount, err)
		}
	}
	native, err := catalog.ParseMarketPrices(source)
	if err != nil {
		t.Fatal(err)
	}
	amount, err := pricing.Price(gateway.Usage{}, native["chat-model"], 1)
	if err != nil || amount != 3 {
		t.Fatalf("native one-micro default changed: amount=%d err=%v", amount, err)
	}
}

func TestMarketPriceProjectionRejectsMalformedAndNegativePrices(t *testing.T) {
	for _, raw := range []string{`[]`, `{"model":null}`, `{"model":[]}`, `{"model":{"input_price_per_million":-1}}`, `{"model":{"billing_mode":"invalid"}}`} {
		if _, err := cmMarketPrices(cmRow{"model_prices": json.RawMessage(raw)}); err == nil {
			t.Fatalf("invalid source prices accepted: %s", raw)
		}
	}
	for _, row := range []cmRow{{}, {"model_prices": json.RawMessage(`null`)}} {
		prices, err := cmMarketPrices(row)
		if err != nil || string(prices) != "{}" {
			t.Fatalf("absent optional prices changed: %s/%v", prices, err)
		}
	}
}
