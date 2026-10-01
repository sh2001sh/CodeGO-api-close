//go:build pgintegration

package ledger

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

func TestPublishedUserFundingPreferencePaysActualSelectedSourceWithoutHotPG(t *testing.T) {
	pool, rdb := testPool(t), testRedis(t)
	wallet := fundedAccount(t, pool, 7, 1000)
	if _, err := pool.Exec(ctx, `INSERT INTO v3_identity.users(id,username,settings) VALUES(7,'published-funding','{"billing_preference":"subscription_only","funding_source_order":["wallet","subscription"],"subscription_order_ids":[1]}');
	 INSERT INTO v3_commerce.plans(id,name,price_minor,credits,period_seconds) VALUES(1,'published',100,1000,3600);
	 INSERT INTO v3_billing.accounts(id,owner_type,owner_id,kind,balance) OVERRIDING SYSTEM VALUE VALUES(43,'subscription',1,'subscription',1000);
	 INSERT INTO v3_commerce.subscriptions(id,user_id,plan_id,account_id,starts_at,expires_at) VALUES(1,7,1,43,now()-interval '1 minute',now()+interval '1 hour')`); err != nil {
		t.Fatal(err)
	}
	var snapshot *catalog.Snapshot
	accounts := NewAccounts(pool)
	settler, err := billing.New(rdb, func() *catalog.Snapshot { return snapshot }, accounts, accounts, billing.Config{}, quiet)
	if err != nil {
		t.Fatal(err)
	}
	if err := accounts.WarmWallets(ctx); err != nil {
		t.Fatal(err)
	}
	if err := settler.WarmBalances(ctx, []int64{wallet, 43}); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"wallet_first", "subscription_only"} {
		if mode == "subscription_only" {
			if _, err := pool.Exec(ctx, `UPDATE v3_identity.users SET settings='{"billing_preference":"subscription_only","funding_source_order":["subscription"],"subscription_order_ids":[1]}' WHERE id=7`); err != nil {
				t.Fatal(err)
			}
		}
		snapshot, err = catalog.Compile(ctx, pool, nil)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.AccountProfiles[7].BillingPreference != mode || len(snapshot.AccountProfiles[7].Subscriptions) != 1 {
			t.Fatalf("actual published user=%#v", snapshot.AccountProfiles[7])
		}
		// Pricing is independent of this test's retained user/settings compiler.
		snapshot.Channels = map[int64]*catalog.Channel{1: {ID: 1}}
		snapshot.Prices = map[string]catalog.Price{"model": {Mode: "per_request", PerRequest: 100}}
		request := &gateway.Request{ID: "published-" + mode, Model: "model", Principal: gateway.Principal{UserID: 7, KeyID: 70, Group: "default"}, Targets: []gateway.Target{{ChannelID: 1, CredentialID: 11, Group: "default", MultiplierPPM: 1_000_000}}}
		before := pool.Stat().AcquireCount()
		if err := settler.Reserve(ctx, request); err != nil {
			t.Fatal(err)
		}
		if err := settler.Finalize(ctx, request, gateway.Outcome{Charge: true, Target: &request.Targets[0], Terminal: gateway.TerminalCompleted}); err != nil {
			t.Fatal(err)
		}
		if after := pool.Stat().AcquireCount(); after != before {
			t.Fatalf("request funding queried PG: %d->%d", before, after)
		}
		messages, err := rdb.XRange(ctx, redisx.StreamBillingEvents, "-", "+").Result()
		if err != nil {
			t.Fatal(err)
		}
		var found *event
		for _, message := range messages {
			e, err := parseEvent(message.ID, message.Values)
			if err != nil {
				t.Fatal(err)
			}
			if e.requestID == request.ID {
				found = &e
				break
			}
		}
		if found == nil || found.fields[billing.FieldFundingPreference] != mode {
			t.Fatalf("preference event=%#v", found)
		}
		want := wallet
		if mode == "subscription_only" {
			want = 43
		}
		if found.accountID != want || found.amount != 100 {
			t.Fatalf("published preference paid wrong source=%+v", found)
		}
		for range 2 {
			if _, err := post(ctx, pool, []event{*found}, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	if balance, version := pgBalance(t, pool, wallet); balance != 900 || version != 1 {
		t.Fatalf("wallet=%d/%d", balance, version)
	}
	if balance, version := pgBalance(t, pool, 43); balance != 900 || version != 1 {
		t.Fatalf("subscription=%d/%d", balance, version)
	}
	var modelUsage int64
	if err := pool.QueryRow(ctx, `SELECT (model_usage->>'model')::bigint FROM v3_commerce.subscriptions WHERE id=1`, pgx.QueryExecModeSimpleProtocol).Scan(&modelUsage); err != nil || modelUsage != 100 {
		t.Fatalf("actual model usage=%d %v", modelUsage, err)
	}
	if count(t, pool, "usage_logs") != 2 || count(t, pool, "ledger_entries") != 2 {
		t.Fatal("settings/replay duplicated actual money")
	}
}
