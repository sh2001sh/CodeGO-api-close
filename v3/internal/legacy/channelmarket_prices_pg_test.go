//go:build pgintegration

package legacy

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/billing/pricing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func assertMigratedMarketPriceQuantum(t *testing.T, target *pgxpool.Pool, importer *Importer, data *channelMarketData) {
	t.Helper()
	ctx := context.Background()
	group := data.channels["legacy-public-201"].group.text("id")
	var raw json.RawMessage
	if err := target.QueryRow(ctx, `SELECT model_prices FROM v3_channelmarket.groups WHERE id=$1`, group).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	prices, err := catalog.ParseMarketPrices(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range []string{"chat-model", "token-model"} {
		amount, priceErr := pricing.Price(gateway.Usage{PromptTokens: 3}, prices[model], 1)
		if priceErr != nil || amount != 4 {
			t.Fatalf("imported %s charge=%d/%v, want old quota half-up x2 = 4", model, amount, priceErr)
		}
	}
	tx, err := target.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Isolate the quantum mismatch from the automatic updated_at change.
	// Both the fixture trigger state and corrupted row are rolled back.
	if _, err = tx.Exec(ctx, `ALTER TABLE v3_channelmarket.groups DISABLE TRIGGER channelmarket_groups_touch`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE v3_channelmarket.groups SET model_prices=jsonb_set(model_prices,'{chat-model,money_quantum}','1') WHERE id=$1`, group); err != nil {
		t.Fatal(err)
	}
	report := Report{}
	err = importer.checkChannelMarket(ctx, tx, data, &report)
	_ = tx.Rollback(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Issues) != 1 || report.Issues[0].Entity != "v3_channelmarket.groups" {
		t.Fatalf("Check missed changed price quantum: %+v", report.Issues)
	}
	var sourceChanged bool
	if err = importer.source.QueryRow(ctx, `SELECT (model_prices::jsonb->'chat-model') ? 'money_quantum' FROM marketplace.channels WHERE id='legacy-public-201'`).Scan(&sourceChanged); err != nil {
		t.Fatal(err)
	}
	if sourceChanged {
		t.Fatal("import changed source price quantum")
	}
}
