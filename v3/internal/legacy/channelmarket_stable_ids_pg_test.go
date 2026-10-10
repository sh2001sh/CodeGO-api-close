//go:build pgintegration

package legacy

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestOnlineGeneratedChannelMappingSurvivesNewChannels(t *testing.T) {
	source, target, crypto := importTestDB(t)
	historyFixture(t, source)
	ctx := context.Background()
	if _, err := source.Exec(ctx, `INSERT INTO migration_source.users(id,username,role,status,"group",quota,claude_quota,setting) VALUES(8,'consumer',1,1,'default',0,0,'{}')`); err != nil {
		t.Fatal(err)
	}
	marketSources := seedChannelMarketFixture(t, source)
	for _, spec := range onlineSpecs(marketSources) {
		keys := make([]string, len(spec.keys))
		for i, key := range spec.keys {
			keys[i] = pgx.Identifier{key}.Sanitize()
		}
		if _, err := source.Exec(ctx, "ALTER TABLE "+spec.source+" ADD PRIMARY KEY ("+strings.Join(keys, ",")+")"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := source.Exec(ctx, `ALTER TABLE marketplace.channels ADD COLUMN base_url_ciphertext text; ALTER TABLE marketplace.channels ADD COLUMN credential_ciphertext text;
	 UPDATE marketplace.channels SET internal_channel_id=0,base_url_ciphertext='https://example.invalid',credential_ciphertext='fixture-upstream-secret'`); err != nil {
		t.Fatal(err)
	}
	m := NewImporter(source, target, crypto)
	opts := OnlineOptions{RunID: "online-stable-market-channel-fixture", SourceAdmin: source}
	if _, err := m.PrepareOnline(ctx, opts, true); err != nil {
		t.Fatal(err)
	}
	if report, err := m.CopyOnline(ctx, opts); err != nil || report.Phase != "copied" {
		t.Fatalf("baseline copy %+v: %v", report, err)
	}
	var originalID, originalGross int64
	stage := onlineStage("v3_channelmarket.settlements")
	if err := target.QueryRow(ctx, "SELECT channel_id,gross_micro FROM "+stage+" WHERE id='settlement-201'").Scan(&originalID, &originalGross); err != nil {
		t.Fatal(err)
	}
	if originalID < 1<<46 || originalID >= 1<<47 || originalGross != 200 {
		t.Fatalf("unsafe generated ID or changed amount: id=%d gross=%d", originalID, originalGross)
	}
	if _, err := source.Exec(ctx, `INSERT INTO migration_source.channels SELECT 14,type,name,key,status,"group",models,base_url FROM migration_source.channels WHERE id=13;
	 INSERT INTO marketplace.channels SELECT (jsonb_populate_record(NULL::marketplace.channels,to_jsonb(c)||jsonb_build_object('id','a-new-public'))).* FROM marketplace.channels c WHERE id='legacy-public-201';
	 INSERT INTO marketplace.groups SELECT (jsonb_populate_record(NULL::marketplace.groups,to_jsonb(g)||jsonb_build_object('id','a-new-group','channel_id','a-new-public','public_slug','new-slug','internal_group_name','new-internal'))).* FROM marketplace.groups g WHERE id='legacy-group-201'`); err != nil {
		t.Fatal(err)
	}
	onlineMigrationSync(t, m, opts)
	var storedID, gross, dependencyID int64
	if err := target.QueryRow(ctx, "SELECT channel_id,gross_micro FROM "+stage+" WHERE id='settlement-201'").Scan(&storedID, &gross); err != nil || storedID != originalID || gross != originalGross {
		t.Fatalf("new channel changed staged history id=%d gross=%d: %v", storedID, gross, err)
	}
	if err := target.QueryRow(ctx, `SELECT (dependencies->'market.channel:legacy-public-201'->>0)::bigint FROM v3_migration_online.run`).Scan(&dependencyID); err != nil || dependencyID != originalID {
		t.Fatalf("new source channels remapped the original dependency: %d %v", dependencyID, err)
	}
	// A real source mapping change still invalidates the old baseline.
	if _, err := source.Exec(ctx, `UPDATE marketplace.channels SET internal_channel_id=14 WHERE id='legacy-public-201'`); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SyncOnline(ctx, opts); err == nil || !strings.Contains(err.Error(), "structural dependency changed") {
		t.Fatalf("true source remapping was accepted: %v", err)
	}
	if _, err := source.Exec(ctx, `UPDATE marketplace.channels SET internal_channel_id=0 WHERE id='legacy-public-201'`); err != nil {
		t.Fatal(err)
	}
	onlineMigrationSync(t, m, opts)
	if r, err := m.VerifyOnline(ctx, opts); err != nil || r.Phase != "verified" {
		t.Fatalf("stable mapping verification=%+v err=%v", r, err)
	}
	if _, err := SealOnlineCapture(ctx, source, opts.RunID); err != nil {
		t.Fatal(err)
	}
	if r, err := m.FinalizeOnline(ctx, opts); err != nil || !r.Applied || len(r.Issues) != 0 {
		t.Fatalf("stable mapping finalization=%+v err=%v", r, err)
	}
	if r, err := m.Check(ctx); err != nil || len(r.Issues) != 0 {
		t.Fatalf("stable mapping independent check=%+v err=%v", r, err)
	}
	var finalID, finalGross, nextID int64
	if err := target.QueryRow(ctx, `SELECT channel_id,gross_micro FROM v3_channelmarket.settlements WHERE id='settlement-201'`).Scan(&finalID, &finalGross); err != nil || finalID != originalID || finalGross != originalGross {
		t.Fatalf("finalized history changed id=%d gross=%d err=%v", finalID, finalGross, err)
	}
	if err := target.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('v3_catalog.channels','id'))`).Scan(&nextID); err != nil || nextID <= originalID || nextID >= 1<<53 {
		t.Fatalf("next channel ID is unsafe or collides: %d err=%v", nextID, err)
	}
}
