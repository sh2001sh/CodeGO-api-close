//go:build pgintegration

package legacy

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

func TestChannelMarketInactiveTimeRangesPreserveSourceAndNeverCreateActiveWindows(t *testing.T) {
	for _, allInactive := range []bool{false, true} {
		t.Run(map[bool]string{false: "mixed", true: "all_inactive"}[allInactive], func(t *testing.T) {
			source, target, crypto := marketMigrationDBs(t)
			ctx := context.Background()
			sources := seedChannelMarketFixture(t, source)
			onlineRecoveryExec(t, source, `CREATE TABLE public.cm_users(id bigint);INSERT INTO public.cm_users VALUES(7),(8),(9);
 CREATE TABLE public.cm_channels(id bigint,"group" text);INSERT INTO public.cm_channels VALUES(13,'default');
 ALTER TABLE marketplace.time_range_multipliers ALTER COLUMN created_at TYPE timestamptz USING created_at::timestamptz;
 ALTER TABLE marketplace.time_range_multipliers ADD COLUMN retained_source_metadata jsonb;
 INSERT INTO marketplace.time_range_multipliers
 SELECT x.* FROM marketplace.time_range_multipliers w
 CROSS JOIN (VALUES('inactive-zero',0::bigint,0::bigint),('inactive-equal',2100000000::bigint,2100000000::bigint),('inactive-inverted',2100000001::bigint,2100000000::bigint))v(id,starts,ends)
 CROSS JOIN LATERAL jsonb_populate_record(NULL::marketplace.time_range_multipliers,
 to_jsonb(w)||jsonb_build_object('id',v.id,'start_timestamp',v.starts,'end_timestamp',v.ends,'multiplier',0.01234567890123456789,
 'retained_source_metadata',jsonb_build_object('original','complete source row')))x
 WHERE w.id='window-201'`)
			if allInactive {
				onlineRecoveryExec(t, source, "UPDATE marketplace.time_range_multipliers SET start_timestamp=0,end_timestamp=0 WHERE id='window-201'")
			}
			sources["users"], sources["channels"] = "public.cm_users", "public.cm_channels"
			onlineRecoveryExec(t, target, `INSERT INTO v3_identity.users(id,username,role,status) VALUES(7,'owner','user','active'),(8,'consumer','user','active'),(9,'window-consumer','user','active');
 INSERT INTO v3_catalog.groups(name) VALUES('default');
 INSERT INTO v3_catalog.channels(id,name,provider,base_url) VALUES(13,'core','openai','https://example.invalid');
 INSERT INTO v3_catalog.channel_groups(channel_id,group_name) VALUES(13,'default');
 INSERT INTO v3_catalog.channel_models(channel_id,model) VALUES(13,'chat-model')`)
			var original, expectedInactive json.RawMessage
			if err := source.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(w) ORDER BY id),
 jsonb_agg(to_jsonb(w) ORDER BY id) FILTER(WHERE end_timestamp<=start_timestamp)
 FROM marketplace.time_range_multipliers w`).Scan(&original, &expectedInactive); err != nil {
				t.Fatal(err)
			}
			read, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = read.Rollback(ctx) }()
			data, err := loadChannelMarket(ctx, read, sources)
			if err != nil || len(data.issues) != 0 {
				t.Fatalf("inactive source rows blocked loading: err=%v data=%+v", err, data)
			}
			sourceReport := Report{}
			data.validate(&sourceReport)
			if sourceReport.Counts["marketplace_time_range_multipliers"] != 4 {
				t.Fatalf("inactive rows disappeared from source count: %+v", sourceReport.Counts)
			}
			importer := NewImporter(source, target, crypto)
			for i := 0; i < 2; i++ {
				write, err := target.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if err := importer.importChannelMarket(ctx, write, data); err != nil {
					_ = write.Rollback(ctx)
					t.Fatal(err)
				}
				if err := write.Commit(ctx); err != nil {
					t.Fatal(err)
				}
			}
			var exact bool
			if err := target.QueryRow(ctx, `SELECT settings #> '{market,legacy_inactive_time_range_multipliers}'=$1::jsonb
 FROM v3_catalog.channels WHERE id=13`, expectedInactive).Scan(&exact); err != nil || !exact {
				t.Fatalf("inactive source rows were not preserved exactly: matched=%t err=%v", exact, err)
			}
			wantActive := 1
			if allInactive {
				wantActive = 0
			}
			var active int
			if err := target.QueryRow(ctx, "SELECT count(*) FROM v3_channelmarket.time_range_multipliers").Scan(&active); err != nil || active != wantActive {
				t.Fatalf("inactive source rows became active windows: count=%d err=%v", active, err)
			}
			check := func() Report {
				t.Helper()
				readTarget, err := target.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = readTarget.Rollback(ctx) }()
				report := Report{}
				if err := importer.checkChannelMarket(ctx, readTarget, data, &report); err != nil {
					t.Fatal(err)
				}
				return report
			}
			if report := check(); len(report.Issues) != 0 {
				t.Fatalf("independent check rejected exact archive: %+v", report.Issues)
			}
			viewTx, err := target.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
			if err != nil {
				t.Fatal(err)
			}
			view, err := catalog.ReadMarketSnapshot(ctx, viewTx)
			_ = viewTx.Rollback(ctx)
			if err != nil {
				t.Fatal(err)
			}
			policy := view.Channels[13]
			wantWindowFactor := "70000"
			if allInactive {
				wantWindowFactor = "75000"
			}
			if policy.FactorExact(9, cmUnix(0)) != "75000" || policy.FactorExact(9, cmUnix(2100000000)) != "75000" || policy.FactorExact(9, cmUnix(1790726401)) != wantWindowFactor {
				t.Fatal("inactive windows changed pricing or valid window lost its original factor")
			}
			onlineRecoveryExec(t, target, `UPDATE v3_catalog.channels SET settings=jsonb_set(settings,
 '{market,legacy_inactive_time_range_multipliers,0,label}','"changed archive"'::jsonb) WHERE id=13`)
			if report := check(); len(report.Issues) == 0 {
				t.Fatal("independent check missed an altered inactive source row")
			}
			onlineRecoveryExec(t, target, `UPDATE v3_catalog.channels SET settings=jsonb_set(settings,
 '{market,legacy_inactive_time_range_multipliers}',$1::jsonb) WHERE id=13`, expectedInactive)
			onlineRecoveryExec(t, target, `INSERT INTO v3_channelmarket.time_range_multipliers
 (id,channel_id,starts_at,ends_at,multiplier_ppm,label) VALUES('invented-window',13,'2026-09-30','2026-10-01',1,'invented')`)
			if report := check(); len(report.Issues) == 0 {
				t.Fatal("independent check missed an invented active window")
			}
			var unchanged json.RawMessage
			if err := source.QueryRow(ctx, "SELECT jsonb_agg(to_jsonb(w) ORDER BY id) FROM marketplace.time_range_multipliers w").Scan(&unchanged); err != nil || string(original) != string(unchanged) {
				t.Fatalf("migration changed source windows: err=%v", err)
			}
		})
	}
}

func TestChannelMarketInactiveTimeRangeTypedZeroFactorStillBlocks(t *testing.T) {
	source, _, _ := marketMigrationDBs(t)
	ctx := context.Background()
	sources := seedChannelMarketFixture(t, source)
	onlineRecoveryExec(t, source, `CREATE TABLE public.cm_users(id bigint);INSERT INTO public.cm_users VALUES(7),(8);
 CREATE TABLE public.cm_channels(id bigint,"group" text);INSERT INTO public.cm_channels VALUES(13,'default');
 UPDATE marketplace.time_range_multipliers SET start_timestamp=0,end_timestamp=0,multiplier=0`)
	sources["users"], sources["channels"] = "public.cm_users", "public.cm_channels"
	read, err := source.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = read.Rollback(ctx) }()
	data, err := loadChannelMarketBase(ctx, read, sources, false)
	if err != nil || len(data.issues) == 0 {
		t.Fatalf("inactive typed row bypassed zero-factor validation: err=%v", err)
	}
}
