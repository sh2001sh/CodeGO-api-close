//go:build pgintegration

package legacy

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestOnlineMarketDeletedGatewayParentKeepsHistoryAndRejectsReactivation(t *testing.T) {
	for _, beforeCopy := range []bool{true, false} {
		name := "deleted_after_copy"
		if beforeCopy {
			name = "already_deleted"
		}
		t.Run(name, func(t *testing.T) {
			source, target, crypto := importTestDB(t)
			historyFixture(t, source)
			ctx := context.Background()
			onlineCaptureTestExec(t, source, `INSERT INTO migration_source.users(id,username,role,status,"group",quota,claude_quota,setting) VALUES(8,'consumer',1,1,'default',0,0,'{}')`)
			seedChannelMarketFixture(t, source)
			// The fixture's official:default pool still needs an independent
			// official gateway after the marketplace gateway is removed.
			onlineCaptureTestExec(t, source, `INSERT INTO migration_source.channels
 SELECT (jsonb_populate_record(NULL::migration_source.channels,to_jsonb(c)||jsonb_build_object('id',14,'name','retained official'))).*
 FROM migration_source.channels c WHERE c.id=13`)
			onlineCaptureTestExec(t, source, `ALTER TABLE marketplace.channels ADD COLUMN deleted_at timestamptz;
 ALTER TABLE marketplace.groups ADD COLUMN deleted_at timestamptz`)
			for _, table := range channelMarketSourceTables {
				if !cmStreamedTable(table) {
					continue
				}
				keys := "id"
				if table == "pelican_artifacts" {
					keys = "group_id,model"
				}
				onlineCaptureTestExec(t, source, "ALTER TABLE "+pgx.Identifier{"marketplace", table}.Sanitize()+" ADD PRIMARY KEY("+keys+")")
			}
			remove := func() {
				onlineCaptureTestExec(t, source, `BEGIN;
 UPDATE marketplace.channels SET deleted_at='2026-10-01T00:00:00Z' WHERE id='legacy-public-201';
 UPDATE marketplace.groups SET deleted_at='2026-10-01T00:00:00Z' WHERE id='legacy-group-201';
 DELETE FROM migration_source.channels WHERE id=13;
 COMMIT`)
			}
			if beforeCopy {
				remove()
			}
			m := NewImporter(source, target, crypto)
			opts := OnlineOptions{RunID: "online-deleted-market-fixture-20261010", SourceAdmin: source}
			onlineMigrationReady(t, m, opts)
			if !beforeCopy {
				remove()
				onlineMigrationSync(t, m, opts)
				if report, err := m.VerifyOnline(ctx, opts); err != nil || report.Phase != "verified" {
					t.Fatalf("deleted parent changed online binding: %+v %v", report, err)
				}
			}
			if _, err := SealOnlineCapture(ctx, source, opts.RunID); err != nil {
				t.Fatal(err)
			}
			if report, err := m.FinalizeOnline(ctx, opts); err != nil || !report.Applied || len(report.Issues) != 0 {
				t.Fatalf("deleted parent finalization: %+v %v", report, err)
			}
			if report, err := m.Check(ctx); err != nil || len(report.Issues) != 0 {
				t.Fatalf("independent deleted history check: %+v %v", report, err)
			}
			var archived bool
			if err := target.QueryRow(ctx, `SELECT c.id=13 AND c.status='disabled' AND c.base_url='' AND c.owner_user_id=7 AND c.scope='marketplace'
 AND g.public_channel_id='legacy-public-201' AND g.lifecycle_status='deleted' AND g.deleted_at='2026-10-01T00:00:00Z'
 AND NOT EXISTS(SELECT 1 FROM v3_catalog.channel_credentials WHERE channel_id=c.id)
 AND EXISTS(SELECT 1 FROM v3_channelmarket.settlements WHERE id='settlement-201' AND group_id=g.id AND net_micro=190)
 AND EXISTS(SELECT 1 FROM v3_community.channel_ratings WHERE channel_id=g.public_channel_id AND legacy_id=98)
 AND EXISTS(SELECT 1 FROM v3_channelmarket.route_pool_members WHERE group_id=g.id)
 AND NOT EXISTS(SELECT 1 FROM pg_constraint WHERE contype='f' AND NOT convalidated AND connamespace='v3_channelmarket'::regnamespace)
 FROM v3_catalog.channels c JOIN v3_channelmarket.groups g ON g.channel_id=c.id WHERE c.id=13`).Scan(&archived); err != nil || !archived {
				t.Fatalf("deleted parent/history/FKs changed: archived=%t err=%v", archived, err)
			}
			// The placeholder is verifiable and cannot be made routable unnoticed.
			onlineCaptureTestExec(t, target, `UPDATE v3_catalog.channels SET status='enabled' WHERE id=13`)
			if report, err := m.Check(ctx); err == nil || len(report.Issues) == 0 {
				t.Fatalf("independent check accepted reactivated archive: %+v %v", report, err)
			}
			snapshot, err := source.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = snapshot.Rollback(ctx) }()
			sources, err := discoverSources(ctx, snapshot)
			if err != nil {
				t.Fatal(err)
			}
			data, err := loadChannelMarket(ctx, snapshot, sources)
			if err != nil || len(data.issues) != 0 {
				t.Fatalf("deleted source changed: %+v %v", data, err)
			}
			write, err := target.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			err = m.importChannelMarket(ctx, write, data)
			_ = write.Rollback(ctx)
			if err == nil || !strings.Contains(err.Error(), "deleted marketplace parent conflicts") {
				t.Fatalf("import overwrote a conflicting target parent: %v", err)
			}
			t.Log("original catalog ID, deleted group, settlements, ratings, route-pool history and validated FKs preserved; enabled archive rejected")
		})
	}
}
