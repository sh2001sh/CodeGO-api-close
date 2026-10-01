//go:build pgintegration

package ledger

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

func TestAcceptedMarketUsageAndIncomeCommitAndRollbackTogether(t *testing.T) {
	pool, rdb := testPool(t), testRedis(t)
	account := fundedAccount(t, pool, 7, 2000)
	_, err := pool.Exec(ctx, `INSERT INTO v3_identity.users(id,username) VALUES(7,'market-consumer'),(8,'market-owner');
	 INSERT INTO v3_catalog.groups(name,multiplier) VALUES('market-hook',1);
	 INSERT INTO v3_catalog.channels(id,name,provider,base_url,scope,owner_user_id) OVERRIDING SYSTEM VALUE VALUES(3,'market-hook','openai','https://example.com','marketplace',8);
	 INSERT INTO v3_channelmarket.groups(id,public_channel_id,channel_id,owner_user_id,public_slug,internal_group_name,display_name,visibility,lifecycle_status)
	 VALUES('market-hook','3',3,8,'market-hook','market-hook','market hook','public','active')`)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := &catalog.Snapshot{Channels: map[int64]*catalog.Channel{3: {ID: 3}},
		Market:          catalog.MarketSnapshot{Channels: map[int64]catalog.MarketChannelPolicy{3: {ModelPrices: map[string]catalog.Price{"model": {Mode: "per_request", PerRequest: 1000}}}}},
		AccountProfiles: map[int64]catalog.AccountProfile{7: {WalletAccountID: account}}}
	accounts := NewAccounts(pool)
	settler, err := billing.New(rdb, func() *catalog.Snapshot { return snapshot }, accounts, accounts, billing.Config{}, quiet)
	if err != nil {
		t.Fatal(err)
	}
	request := &gateway.Request{ID: "market-ledger", Model: "model", Principal: gateway.Principal{UserID: 7, KeyID: 70, Group: "market-hook"}, Targets: []gateway.Target{{ChannelID: 3, CredentialID: 30, Group: "market-hook", MultiplierPPM: 1000000}}}
	if err := settler.Reserve(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := settler.Finalize(ctx, request, gateway.Outcome{Charge: true, Terminal: gateway.TerminalCompleted, Target: &request.Targets[0]}); err != nil {
		t.Fatal(err)
	}
	messages, err := rdb.XRange(ctx, redisx.StreamBillingEvents, "-", "+").Result()
	if err != nil || len(messages) != 1 {
		t.Fatalf("market event: %v %v", messages, err)
	}
	e, err := parseEvent(messages[0].ID, messages[0].Values)
	if err != nil {
		t.Fatal(err)
	}
	market := channelmarket.New(pool, nil, NewPoster(pool, rdb), channelmarket.Config{}, quiet)
	failedHook := func(ctx context.Context, tx pgx.Tx, fields map[string]string) error {
		if err := market.AccrueUsageTx(ctx, tx, fields); err != nil {
			return err
		}
		return errors.New("market posting failed after owner credit")
	}
	if _, err = post(ctx, pool, []event{e}, nil, failedHook); err == nil {
		t.Fatal("hook failure committed a charge")
	}
	if balance, _ := pgBalance(t, pool, account); balance != 2000 {
		t.Fatalf("failed market hook consumed wallet=%d", balance)
	}
	var settlements int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_channelmarket.settlements`).Scan(&settlements); err != nil || settlements != 0 {
		t.Fatalf("failed market hook retained owner income=%d %v", settlements, err)
	}
	for range 2 {
		if _, err = post(ctx, pool, []event{e}, nil, market.AccrueUsageTx); err != nil {
			t.Fatal(err)
		}
	}
	var consumer, gross, commission, net, factor int64
	var source string
	if err = pool.QueryRow(ctx, `SELECT consumer_micro,gross_micro,commission_micro,net_micro,multiplier_ppm,billing_source FROM v3_channelmarket.settlements WHERE request_id='market-ledger'`).Scan(&consumer, &gross, &commission, &net, &factor, &source); err != nil {
		t.Fatal(err)
	}
	if consumer != 1000 || gross != 1000 || commission != 50 || net != 950 || factor != 1000000 || source != "wallet" {
		t.Fatalf("market income=%d/%d/%d/%d/%d/%s", consumer, gross, commission, net, factor, source)
	}
	if balance, version := pgBalance(t, pool, account); balance != 1000 || version != 1 {
		t.Fatalf("market wallet=%d/%d", balance, version)
	}
	if count(t, pool, "usage_logs") != 1 || count(t, pool, "ledger_entries") != 3 {
		t.Fatal("market redelivery duplicated money or usage")
	}
}
