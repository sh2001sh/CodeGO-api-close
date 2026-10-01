//go:build pgintegration

package legacy

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Exercise both linked-channel UPDATE and draft-channel INSERT projections.
func assertMigratedMarketCardFlags(t *testing.T, target *pgxpool.Pool, importer *Importer, data *channelMarketData) {
	t.Helper()
	ctx := context.Background()
	channel := data.channels["legacy-public-201"]
	var supported, enabled bool
	if err := target.QueryRow(ctx, `SELECT multiplier_card_supported,
	 (settings#>>'{market,multiplier_card_user_enabled}')::boolean
	 FROM v3_catalog.channels WHERE id=$1`, channel.catalogID).Scan(&supported, &enabled); err != nil {
		t.Fatal(err)
	}
	if !supported || enabled {
		t.Fatalf("source capability/activation collapsed: supported=%t enabled=%t, want true/false", supported, enabled)
	}
	for _, change := range []struct{ mutate, restore, detail string }{
		{`UPDATE v3_catalog.channels SET multiplier_card_supported=false WHERE id=$1`,
			`UPDATE v3_catalog.channels SET multiplier_card_supported=true WHERE id=$1`, "capability"},
		{`UPDATE v3_catalog.channels SET settings=jsonb_set(settings,'{market,multiplier_card_user_enabled}','true') WHERE id=$1`,
			`UPDATE v3_catalog.channels SET settings=jsonb_set(settings,'{market,multiplier_card_user_enabled}','false') WHERE id=$1`, "activation"},
	} {
		if _, err := target.Exec(ctx, change.mutate, channel.catalogID); err != nil {
			t.Fatal(err)
		}
		tx, err := target.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
		if err != nil {
			t.Fatal(err)
		}
		report := Report{}
		err = importer.checkChannelMarket(ctx, tx, data, &report)
		_ = tx.Rollback(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(report.Issues) != 1 || !strings.Contains(report.Issues[0].Detail, "multiplier-card "+change.detail) {
			t.Fatalf("Check missed changed native %s: %+v", change.detail, report.Issues)
		}
		if _, err = target.Exec(ctx, change.restore, channel.catalogID); err != nil {
			t.Fatal(err)
		}
	}
}
