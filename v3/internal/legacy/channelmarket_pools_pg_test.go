//go:build pgintegration

package legacy

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
)

func TestChannelMarketMissingOfficialPoolMembersRemainDormantAndCheckable(t *testing.T) {
	source, target, crypto := marketMigrationDBs(t)
	ctx := context.Background()
	sources := seedChannelMarketFixture(t, source)
	onlineRecoveryExec(t, source, `CREATE TABLE public.cm_users(id bigint,"group" text);
 INSERT INTO public.cm_users VALUES(7,'default'),(8,'retired');
 CREATE TABLE public.cm_channels(id bigint,"group" text);INSERT INTO public.cm_channels VALUES(13,'default');
 UPDATE marketplace.route_pool_members SET group_id='official:retired',priority=5;
 UPDATE marketplace.auto_route_pool_members SET group_id='official:retired',priority=9`)
	sources["users"], sources["channels"] = "public.cm_users", "public.cm_channels"
	onlineRecoveryExec(t, target, `INSERT INTO v3_identity.users(id,username,role,status,group_name)
 VALUES(7,'owner','user','active','default'),(8,'consumer','user','active','retired');
 INSERT INTO v3_catalog.groups(name) VALUES('default');
 INSERT INTO v3_catalog.channels(id,name,provider,base_url) VALUES(13,'core','openai','https://example.invalid');
 INSERT INTO v3_catalog.channel_groups(channel_id,group_name) VALUES(13,'default');
 INSERT INTO v3_catalog.channel_models(channel_id,model) VALUES(13,'chat-model')`)
	read, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = read.Rollback(ctx) }()
	data, err := loadChannelMarket(ctx, read, sources)
	if err != nil || len(data.issues) != 0 {
		t.Fatalf("missing official source groups blocked typed loading: err=%v data=%+v", err, data)
	}
	importer := NewImporter(source, target, crypto)
	apply := func() {
		t.Helper()
		write, err := target.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = write.Rollback(ctx) }()
		if err := importer.importChannelMarket(ctx, write, data); err != nil {
			t.Fatal(err)
		}
		if err := write.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	apply()
	apply()
	var retained, active, invented, sourceUsers int
	if err := target.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM v3_channelmarket.route_pool_members WHERE group_id='official:retired' AND catalog_group_name IS NULL AND legacy_id=1
 AND ((legacy_source='route_pool_members' AND pool_id='pool-201' AND priority=5)
 OR (legacy_source='auto_route_pool_members' AND pool_id=$1 AND priority=9))),
 (SELECT count(*) FROM v3_catalog.route_pool_members),
 (SELECT count(*) FROM v3_catalog.groups WHERE name='retired'),
 (SELECT count(*) FROM v3_identity.users WHERE id=8 AND group_name='retired')`, cmAutoID(8)).Scan(&retained, &active, &invented, &sourceUsers); err != nil || retained != 2 || active != 0 || invented != 0 || sourceUsers != 1 {
		t.Fatalf("retained=%d active=%d invented=%d source_users=%d err=%v", retained, active, invented, sourceUsers, err)
	}
	check := func() Report {
		t.Helper()
		tx, err := target.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		report := Report{}
		if err := importer.checkChannelMarket(ctx, tx, data, &report); err != nil {
			t.Fatal(err)
		}
		return report
	}
	if report := check(); len(report.Issues) != 0 {
		t.Fatalf("independent check rejected preserved pool members: %+v", report.Issues)
	}
	for _, mutation := range []string{
		`legacy_id=100`, `priority=6`, `group_id='official:changed'`, `catalog_group_name='default'`,
	} {
		onlineRecoveryExec(t, target, `UPDATE v3_channelmarket.route_pool_members SET `+mutation+` WHERE pool_id='pool-201'`)
		if report := check(); len(report.Issues) == 0 {
			t.Fatalf("independent check missed altered pool member: %s", mutation)
		}
		onlineRecoveryExec(t, target, `UPDATE v3_channelmarket.route_pool_members SET legacy_id=1,priority=5,group_id='official:retired',catalog_group_name=NULL WHERE pool_id='pool-201'`)
	}
	service := channelmarket.New(target, crypto, nil, channelmarket.Config{}, nil)
	before, err := service.Pools(ctx, 8)
	if err != nil || len(before) != 2 {
		t.Fatalf("retained pool configuration is unreadable: %v %+v", err, before)
	}
	// Even if the target later gains a real official channel, replaying this
	// source snapshot must not activate its formerly missing configured group.
	onlineRecoveryExec(t, target, `INSERT INTO v3_catalog.groups(name) VALUES('retired');
 INSERT INTO v3_catalog.channels(id,name,provider,base_url,scope) VALUES(21,'restored-official','openai','https://example.invalid','official');
 INSERT INTO v3_catalog.channel_groups(channel_id,group_name) VALUES(21,'retired');
 INSERT INTO v3_catalog.channel_models(channel_id,model) VALUES(21,'chat-model')`)
	apply()
	after, err := service.Pools(ctx, 8)
	if err != nil {
		t.Fatal(err)
	}
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	if string(beforeJSON) != string(afterJSON) {
		t.Fatal("restoring a real official group lost the original pool configuration")
	}
	if err := target.QueryRow(ctx, "SELECT count(*) FROM v3_catalog.route_pool_members").Scan(&active); err != nil || active != 0 {
		t.Fatalf("target-only official group activated a dormant source member: active=%d err=%v", active, err)
	}
	if report := check(); len(report.Issues) != 0 {
		t.Fatalf("restored official group changed retained member projection: %+v", report.Issues)
	}
	var sourceUnchanged bool
	if err := source.QueryRow(ctx, `SELECT
 EXISTS(SELECT 1 FROM marketplace.route_pool_members WHERE id=1 AND pool_id='pool-201' AND group_id='official:retired' AND priority=5)
 AND EXISTS(SELECT 1 FROM marketplace.auto_route_pool_members WHERE id=1 AND owner_user_id=8 AND group_id='official:retired' AND priority=9)
 AND EXISTS(SELECT 1 FROM public.cm_users WHERE id=8 AND "group"='retired')`).Scan(&sourceUnchanged); err != nil || !sourceUnchanged {
		t.Fatalf("migration changed source configuration: unchanged=%t err=%v", sourceUnchanged, err)
	}
}

func TestChannelMarketPoolBadConfigurationTypedSourceStillBlocks(t *testing.T) {
	source, _, _ := marketMigrationDBs(t)
	ctx := context.Background()
	sources := seedChannelMarketFixture(t, source)
	onlineRecoveryExec(t, source, `CREATE TABLE public.cm_users(id bigint);INSERT INTO public.cm_users VALUES(7),(8);
 CREATE TABLE public.cm_channels(id bigint,"group" text);INSERT INTO public.cm_channels VALUES(13,'default')`)
	sources["users"], sources["channels"] = "public.cm_users", "public.cm_channels"
	for _, mutation := range []string{
		`pool_id='missing',group_id='official:retired'`,
		`group_id='missing-market-group'`,
		`group_id='official:'`,
		`group_id='official: ',priority=1`,
		`group_id='official:retired',priority=2147483648`,
	} {
		onlineRecoveryExec(t, source, `UPDATE marketplace.route_pool_members SET `+mutation)
		read, err := source.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
		if err != nil {
			t.Fatal(err)
		}
		data, err := loadChannelMarketBase(ctx, read, sources, false)
		_ = read.Rollback(ctx)
		if err != nil || len(data.issues) == 0 {
			t.Fatalf("invalid typed pool configuration was not rejected: mutation=%s err=%v data=%+v", mutation, err, data)
		}
		onlineRecoveryExec(t, source, `UPDATE marketplace.route_pool_members SET pool_id='pool-201',group_id='legacy-group-201',priority=1`)
	}
}
