//go:build pgintegration

package legacy

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestChannelMarketStreamsAcrossBatchesAndChecksEveryKey(t *testing.T) {
	source, target, crypto := marketMigrationDBs(t)
	ctx := context.Background()
	sources := seedChannelMarketFixture(t, source)
	_, err := source.Exec(ctx, `CREATE TABLE public.cm_users(id bigint);
		INSERT INTO public.cm_users VALUES(7),(8);
		CREATE TABLE public.cm_channels(id bigint,"group" text);
		INSERT INTO public.cm_channels VALUES(13,'default');
		INSERT INTO marketplace.settlements
		SELECT (jsonb_populate_record(NULL::marketplace.settlements,to_jsonb(s)||
			jsonb_build_object('id','bulk-'||n,'request_id','bulk-request-'||n,'status','released'))).*
		FROM marketplace.settlements s CROSS JOIN generate_series(1,1536) n
		WHERE s.id='settlement-201'`)
	if err != nil {
		t.Fatal(err)
	}
	sources["users"], sources["channels"] = "public.cm_users", "public.cm_channels"
	if _, err = target.Exec(ctx, `INSERT INTO v3_identity.users(id,username,role,status) VALUES(7,'owner','user','active'),(8,'consumer','user','active');
		INSERT INTO v3_catalog.groups(name) VALUES('default');
		INSERT INTO v3_catalog.channels(id,name,provider,base_url) VALUES(13,'core','openai','https://example.invalid');
		INSERT INTO v3_catalog.channel_groups(channel_id,group_name) VALUES(13,'default')`); err != nil {
		t.Fatal(err)
	}
	snapshot, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = snapshot.Rollback(ctx) }()
	data, err := loadChannelMarket(ctx, snapshot, sources)
	if err != nil {
		t.Fatal(err)
	}
	if data.streamCounts["settlements"] != 1537 || len(data.rows["settlements"]) != 0 || len(data.rows["multiplier_trend_snapshots"]) != 0 {
		t.Fatalf("large tables were retained or skipped: counts=%v", data.streamCounts)
	}
	for _, record := range data.records {
		if record.table == "v3_channelmarket.settlements" || record.table == "v3_channelmarket.multiplier_trend_snapshots" {
			t.Fatal("historical stream was retained in projected records")
		}
	}
	var batches, records int
	if err = data.streamBatches(ctx, "settlements", func(batch []cmRecord) error {
		if len(batch) > 512 {
			t.Fatal("batch memory bound exceeded")
		}
		batches++
		records += len(batch)
		return nil
	}); err != nil || records != 1537 || batches != 4 {
		t.Fatalf("stream records=%d batches=%d err=%v", records, batches, err)
	}
	importer := NewImporter(source, target, crypto)
	for replay := 0; replay < 2; replay++ {
		tx, err := target.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = importer.importChannelMarket(ctx, tx, data); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	check := func() Report {
		tx, err := target.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		report := Report{}
		if err = importer.checkChannelMarket(ctx, tx, data, &report); err != nil {
			t.Fatal(err)
		}
		return report
	}
	if report := check(); len(report.Issues) != 0 || report.Counts["check:v3_channelmarket.settlements"] != 1537 {
		t.Fatalf("exact read-only check failed: %+v", report)
	}
	// Same total record count, but an amount changed past the first two batches.
	if _, err = target.Exec(ctx, `UPDATE v3_channelmarket.settlements SET consumer_micro=consumer_micro+2 WHERE id='bulk-1025'`); err != nil {
		t.Fatal(err)
	}
	if report := check(); len(report.Issues) != 1 {
		t.Fatalf("changed late-batch amount was not detected: %+v", report.Issues)
	}
	tx, err := target.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = importer.importChannelMarket(ctx, tx, data)
	_ = tx.Rollback(ctx)
	if err == nil {
		t.Fatal("changed replay was accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	err = data.streamBatches(cancelled, "settlements", func([]cmRecord) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled source stream was ignored: %v", err)
	}
}
