//go:build pgintegration

package legacy

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing/pricing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/routing"
)

func TestChannelMarketImportedPriorityPoolPreservesMemberOrder(t *testing.T) {
	source, target, crypto := marketMigrationDBs(t)
	ctx := context.Background()
	sources := seedChannelMarketFixture(t, source)
	if _, err := source.Exec(ctx, `CREATE TABLE public.cm_users(id bigint);
	 INSERT INTO public.cm_users VALUES(7),(8);
	 CREATE TABLE public.cm_channels(id bigint,"group" text);
	 INSERT INTO public.cm_channels VALUES(13,'default'),(21,'other');
	 INSERT INTO marketplace.route_pools(id,owner_user_id,name,strategy,max_attempts,max_multiplier,failure_cooldown_seconds)
	 VALUES('priority-201',7,'Priority pool','priority',3,1,30);
	 INSERT INTO marketplace.route_pool_members(id,pool_id,group_id,priority)
	 VALUES(2,'priority-201','legacy-group-201',1),(3,'priority-201','official:other',9)`); err != nil {
		t.Fatal(err)
	}
	sources["users"], sources["channels"] = "public.cm_users", "public.cm_channels"
	if _, err := target.Exec(ctx, `INSERT INTO v3_identity.users(id,username,role,status)
	 VALUES(7,'owner','user','active'),(8,'consumer','user','active');
	 INSERT INTO v3_catalog.groups(name) VALUES('default'),('other');
	 INSERT INTO v3_platform.settings(key,value) VALUES('UserUsableGroups','{"default":"Default","other":"Other"}');
	 INSERT INTO v3_catalog.channels(id,name,provider,base_url)
	 VALUES(13,'core','openai','https://example.invalid'),(21,'other','openai','https://example.invalid');
	 INSERT INTO v3_catalog.channel_groups(channel_id,group_name) VALUES(13,'default'),(21,'other');
	 INSERT INTO v3_catalog.channel_models(channel_id,model) VALUES(13,'chat-model'),(21,'chat-model')`); err != nil {
		t.Fatal(err)
	}
	secret, err := crypto.Encrypt([]byte("fixture-upstream-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = target.Exec(ctx, `INSERT INTO v3_catalog.channel_credentials(channel_id,secret) VALUES(13,$1),(21,$1)`, secret); err != nil {
		t.Fatal(err)
	}
	sourceTx, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sourceTx.Rollback(ctx) }()
	data, err := loadChannelMarket(ctx, sourceTx, sources)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.issues) != 0 {
		t.Fatalf("preflight issues=%+v", data.issues)
	}
	tx, err := target.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	importer := NewImporter(source, target, crypto)
	if err = importer.importChannelMarket(ctx, tx, data); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	snapshot, err := catalog.Compile(ctx, target, crypto)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Channels[13].MultiplierCardSupported || snapshot.Channels[13].MultiplierCardUserEnabled {
		t.Fatal("compiled runtime collapsed source card capability and activation")
	}
	for _, model := range []string{"chat-model", "token-model"} {
		price := snapshot.Market.Channels[13].ModelPrices[model]
		amount, priceErr := pricing.Price(gateway.Usage{PromptTokens: 3}, price, 1)
		if priceErr != nil || amount != 4 {
			t.Fatalf("real imported %s quota half-up x2 charge=%d/%v, want 4", model, amount, priceErr)
		}
	}
	if snapshot.Market.Channels[13].ModelPrices["token-model"].CacheWritePerMTok != 9007199254740993 {
		t.Fatal("compiled imported cache price lost integer precision")
	}
	planner := routing.New(func() *catalog.Snapshot { return snapshot }, routing.Config{})
	request := &gateway.Request{Model: "chat-model", Protocol: gateway.ProtocolOpenAIChat,
		Principal: gateway.Principal{UserID: 7, Group: "pool_priority-201", AllowedGroups: []string{"pool_priority-201", "other"}}}
	plan, err := planner.Plan(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 2 || plan[0].ChannelID != 13 || plan[1].ChannelID != 21 {
		ids := make([]int64, len(plan))
		for i := range plan {
			ids[i] = plan[i].ChannelID
		}
		t.Fatalf("imported ascending priority planner order=%v, want [13 21]", ids)
	}
	members := snapshot.Market.Pools["pool_priority-201"].Members
	if len(members) != 2 || members[0].Priority != 1 || members[1].Priority != 9 {
		t.Fatalf("native operational priorities changed: %+v", members)
	}
	var first, second int
	if err = target.QueryRow(ctx, `SELECT
	 (SELECT m.priority FROM v3_catalog.route_pool_members m JOIN v3_catalog.route_pools p ON p.id=m.pool_id WHERE p.group_name='pool_priority-201' AND m.channel_id=13),
	 (SELECT m.priority FROM v3_catalog.route_pool_members m JOIN v3_catalog.route_pools p ON p.id=m.pool_id WHERE p.group_name='pool_priority-201' AND m.channel_id=21)`).Scan(&first, &second); err != nil {
		t.Fatal(err)
	}
	if first != -1 || second != -9 {
		t.Fatalf("compiled catalog priorities=%d,%d, want -1,-9", first, second)
	}
	request.Principal.UserID = 8
	if plan, err = planner.Plan(ctx, request); err == nil || len(plan) != 0 {
		t.Fatal("imported personal pool allowed a different owner")
	}
}
