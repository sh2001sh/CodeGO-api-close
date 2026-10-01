//go:build pgintegration

package legacy

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func seedCatalogDataFixture(t *testing.T, source *pgxpool.Pool) {
	t.Helper()
	_, err := source.Exec(context.Background(), `
	CREATE TABLE migration_source.vendors(id bigint PRIMARY KEY,name text,description text,icon text,status int,created_time bigint,updated_time bigint,deleted_at timestamptz);
	CREATE TABLE migration_source.models(id bigint PRIMARY KEY,model_name text,description text,icon text,tags text,endpoints text,vendor_id bigint,status int,sync_official int,name_rule int,created_time bigint,updated_time bigint,deleted_at timestamptz);
	CREATE TABLE migration_source.prefill_groups(id bigint PRIMARY KEY,name text,type text,items jsonb,description text,created_time bigint,updated_time bigint,deleted_at timestamptz);
	CREATE TABLE migration_source.route_pools(id bigint PRIMARY KEY,name text,"group" text,enabled bool,model_scope text,auto_discover bool,multiplier_weight int,ttft_weight int,cache_weight int,success_weight int,deleted_at timestamptz);
	CREATE TABLE migration_source.route_pool_members(id bigint PRIMARY KEY,route_pool_id bigint,channel_id bigint,cost_multiplier numeric,model_cost_overrides text,fault_domain text,enabled bool,deleted_at timestamptz);
	INSERT INTO migration_source.vendors VALUES(21,'Fixture Vendor','active description','icon',1,1700000000,1700000001,NULL),(22,'Fixture Vendor','past description','old-icon',2,1600000000,1600000001,'2026-09-29T12:00:00Z');
	INSERT INTO migration_source.models VALUES(31,'chat','live model','icon','tag','{"chat":{"path":"/v1/chat/completions"}}',21,1,1,1,1700000000,1700000001,NULL),(32,'old-chat','deleted model','old-icon','','{}',22,2,0,0,1600000000,1600000001,'2026-09-29T12:00:00Z');
	INSERT INTO migration_source.prefill_groups VALUES(41,'fixture-models','model','["chat-model"]','reusable models',1700000000,1700000001,NULL),(42,'fixture-endpoints','endpoint','{"chat":{"path":"/v1/chat/completions"}}','reusable endpoints',1700000000,1700000001,NULL);
	INSERT INTO migration_source.route_pools VALUES(51,'official-fixture','default',true,'chat-model',true,35,25,15,25,NULL),(52,'disabled-fixture','default',false,'chat-model',false,10,20,30,40,NULL);
	INSERT INTO migration_source.route_pool_members VALUES(61,51,13,0.123456789012345678,'{"chat-model":0.999999999999999999}','shared-host',true,NULL),(62,52,13,1.25,'{}','historical-host',false,'2026-09-29T12:00:00Z');`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestCatalogDataSeparateDatabasesReplayAndReconciliation(t *testing.T) {
	source, target, crypto := importTestDB(t)
	seedCatalogDataFixture(t, source)
	ctx := context.Background()
	importer := NewImporter(readonlySource(t, source), target, crypto)
	report, err := importer.Import(ctx, false)
	if err != nil || len(report.Issues) != 0 || report.Counts["route_pool_members"] != 2 {
		t.Fatalf("dry run=%+v err=%v", report, err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if report, err = importer.Import(ctx, true); err != nil || !report.Applied {
			t.Fatalf("apply=%+v err=%v", report, err)
		}
	}
	var cost, override, strategy string
	var discover bool
	if err = target.QueryRow(ctx, `SELECT m.cost_multiplier::text,m.model_cost_overrides->>'chat-model',p.strategy,p.auto_discover FROM v3_catalog.route_pool_members m JOIN v3_catalog.route_pools p ON p.id=m.pool_id WHERE m.legacy_id=61`).Scan(&cost, &override, &strategy, &discover); err != nil {
		t.Fatal(err)
	}
	if cost != "0.123456789012345678" || override != "0.999999999999999999" || strategy != "scored" || !discover {
		t.Fatalf("cost=%s override=%s strategy=%s discover=%v", cost, override, strategy, discover)
	}
	if report, err = importer.Check(ctx); err != nil || len(report.Issues) != 0 || report.Counts["check:catalog:models:matched"] != 2 {
		t.Fatalf("check=%+v err=%v", report, err)
	}
	if _, err = target.Exec(ctx, `UPDATE v3_catalog.route_pool_members SET cost_multiplier=2 WHERE legacy_id=61`); err != nil {
		t.Fatal(err)
	}
	if report, err = importer.Check(ctx); err == nil || len(report.Issues) == 0 {
		t.Fatalf("check missed changed member cost: %+v %v", report, err)
	}
	if report, err = importer.Import(ctx, true); err == nil || report.Applied {
		t.Fatalf("replay overwrote changed member cost: %+v %v", report, err)
	}
}

func TestCatalogDataRejectsDanglingVendorBeforeWrites(t *testing.T) {
	source, target, crypto := importTestDB(t)
	seedCatalogDataFixture(t, source)
	ctx := context.Background()
	if _, err := source.Exec(ctx, `UPDATE migration_source.models SET vendor_id=999 WHERE id=31`); err != nil {
		t.Fatal(err)
	}
	report, err := NewImporter(readonlySource(t, source), target, crypto).Import(ctx, true)
	if err == nil || report.Applied || len(report.Issues) != 1 || report.Issues[0].Code != "catalog_vendor_missing" {
		t.Fatalf("invalid source=%+v err=%v", report, err)
	}
	var count int64
	if err = target.QueryRow(ctx, `SELECT count(*) FROM v3_identity.users`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid source wrote core users: %d %v", count, err)
	}
}
