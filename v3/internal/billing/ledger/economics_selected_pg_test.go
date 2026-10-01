//go:build pgintegration

package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/routing"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

func TestSelectedProcurementReachesEconomicsWithoutRepricingOrDoubleDebit(t *testing.T) {
	pool, rdb := testPool(t), testRedis(t)
	account := fundedAccount(t, pool, 7, 2000)
	snapshot := &catalog.Snapshot{Groups: map[string]catalog.Group{"default": {Multiplier: 0.4}},
		Channels: map[int64]*catalog.Channel{3: {ID: 3, Provider: "openai", BaseURL: "http://test.invalid",
			Credentials: []catalog.Credential{{ID: 30, ChannelID: 3, Secret: "test-only"}}}},
		Routes:          map[string]map[string][]catalog.Route{"default": {"model": {{ChannelID: 3, Weight: 1, Strategy: "weighted"}}}},
		Prices:          map[string]catalog.Price{"model": {Mode: "per_request", PerRequest: 1000}},
		AccountProfiles: map[int64]catalog.AccountProfile{7: {WalletAccountID: account}},
		OfficialPools: map[string]catalog.OfficialPool{"default": {ID: 17, Group: "default", MultiplierWeight: 100,
			Members: []catalog.OfficialPoolMember{{ChannelID: 3, Models: []string{"model"}, CostMultiplier: "0.7", ModelCostOverrides: map[string]json.Number{"model": "0.2"}}}}}}
	request := &gateway.Request{ID: "selected-economics", Model: "model", Principal: gateway.Principal{UserID: 7, KeyID: 70, Group: "default"}}
	targets, err := routing.New(func() *catalog.Snapshot { return snapshot }, routing.Config{}).Plan(ctx, request)
	if err != nil || len(targets) != 1 || targets[0].RoutePoolID != 17 || targets[0].ProcurementCostMultiplier != "0.2" {
		t.Fatalf("actual selected procurement=%#v %v", targets, err)
	}
	request.Targets = targets
	accounts := NewAccounts(pool)
	settler, err := billing.New(rdb, func() *catalog.Snapshot { return snapshot }, accounts, accounts, billing.Config{}, quiet)
	if err != nil {
		t.Fatal(err)
	}
	if err := settler.Reserve(ctx, request); err != nil {
		t.Fatal(err)
	}
	// The completion target and current publication cannot overwrite admission facts.
	request.Targets[0].RoutePoolID, request.Targets[0].ProcurementCostMultiplier = 99, "999"
	snapshot.Prices["model"] = catalog.Price{Mode: "per_request", PerRequest: 9999}
	if err := settler.Finalize(ctx, request, gateway.Outcome{Charge: true, Target: &request.Targets[0], Terminal: gateway.TerminalCompleted}); err != nil {
		t.Fatal(err)
	}
	messages, err := rdb.XRange(ctx, redisx.StreamBillingEvents, "-", "+").Result()
	if err != nil || len(messages) != 1 {
		t.Fatalf("accepted event=%v %v", messages, err)
	}
	e, err := parseEvent(messages[0].ID, messages[0].Values)
	if err != nil {
		t.Fatal(err)
	}
	failed := func(context.Context, pgx.Tx, map[string]string) error {
		return errors.New("economic transaction rejected")
	}
	if _, err := post(ctx, pool, []event{e}, nil, failed); err == nil {
		t.Fatal("rejected event committed")
	}
	var rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_billing.request_economics`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("rollback retained economics=%d %v", rows, err)
	}
	for range 2 {
		if _, err := post(ctx, pool, []event{e}, nil); err != nil {
			t.Fatal(err)
		}
	}
	var channel, poolID, actual, cost int64
	var source string
	if err := pool.QueryRow(ctx, `SELECT channel_id,route_pool_id,actual_amount,procurement_cost_multiplier_ppm,billing_source FROM v3_billing.request_economics WHERE request_id=$1`, request.ID).Scan(&channel, &poolID, &actual, &cost, &source); err != nil {
		t.Fatal(err)
	}
	if channel != 3 || poolID != 17 || actual != 400 || cost != 200000 || source != "wallet" {
		t.Fatalf("frozen economics=%d/%d/%d/%d/%s", channel, poolID, actual, cost, source)
	}
	if balance, version := pgBalance(t, pool, account); balance != 1600 || version != 1 {
		t.Fatalf("economic metadata charged extra=%d/%d", balance, version)
	}
	if count(t, pool, "usage_logs") != 1 || count(t, pool, "ledger_entries") != 1 {
		t.Fatal("economics retry duplicated billing")
	}
}
