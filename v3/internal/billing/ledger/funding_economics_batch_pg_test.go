//go:build pgintegration

package ledger

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/billing"
)

func economicsBatchEvent(t *testing.T, account int64, request string, amount int64, fields map[string]string) event {
	t.Helper()
	e := usageHookEvent(t, account, request, amount, false)
	for key, value := range fields {
		e.fields[key] = value
	}
	e.fingerprint = fingerprint(e.fields)
	return e
}

func economicsBatchWalletEvent(t *testing.T, account int64, request string, amount int64) event {
	t.Helper()
	return economicsBatchEvent(t, account, request, amount, map[string]string{
		"funding_policy_version":          "wallet",
		"funding_wallet_equivalent_micro": strconv.FormatInt(amount, 10),
		billing.FieldTimestamp:            "1791374400123",
	})
}

func assertEconomicsBatchRolledBack(t *testing.T, pool *pgxpool.Pool, economics int, accounts ...int64) {
	t.Helper()
	for _, account := range accounts {
		if balance, version := pgBalance(t, pool, account); balance != 1000 || version != 0 {
			t.Fatalf("failed batch changed account %d: balance/version=%d/%d", account, balance, version)
		}
	}
	for _, table := range []string{"billing_dedup", "ledger_entries", "usage_logs", "funding_lots", "funding_allocations", "funding_source_usage"} {
		if rows := count(t, pool, table); rows != 0 {
			t.Fatalf("failed batch retained %s rows=%d", table, rows)
		}
	}
	if rows := count(t, pool, "request_economics"); rows != economics {
		t.Fatalf("failed batch changed economics rows=%d, want %d", rows, economics)
	}
}

func TestEconomicsBatchPreservesExactSnapshotsAndOneInsert(t *testing.T) {
	pool := testPool(t)
	const large = int64(9007199254740993)
	wallet := fundedAccount(t, pool, 7, large+7)
	sub := fundedAccount(t, pool, 8, 1000)
	if _, err := pool.Exec(ctx, `UPDATE v3_billing.accounts SET owner_type='subscription',owner_id=1,kind='subscription' WHERE id=$1`, sub); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO v3_billing.funding_source_policies(source,revenue_multiplier_ppm) VALUES('topup',700000),('subscription',300000)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO v3_billing.funding_lots
	 (lot_id,source_account_id,account_id,source,idempotency_key,original_amount,remaining_amount,revenue_multiplier_ppm)
	 VALUES('batch-paid','source-wallet',$1,'topup','batch-paid',$2,$2,700000)`, wallet, large+7); err != nil {
		t.Fatal(err)
	}
	// This observes the optimization's database statement count as well as its
	// results: restoring per-request inserts would make this assertion fail.
	if _, err := pool.Exec(ctx, `CREATE TABLE v3_billing.economics_insert_statements(n integer NOT NULL);
	 INSERT INTO v3_billing.economics_insert_statements VALUES(0);
	 CREATE FUNCTION v3_billing.count_economics_statement() RETURNS trigger LANGUAGE plpgsql AS $$
	 BEGIN UPDATE v3_billing.economics_insert_statements SET n=n+1; RETURN NULL; END $$;
	 CREATE TRIGGER count_economics_statement AFTER INSERT ON v3_billing.request_economics
	 FOR EACH STATEMENT EXECUTE FUNCTION v3_billing.count_economics_statement()`); err != nil {
		t.Fatal(err)
	}
	first := economicsBatchEvent(t, wallet, "batch-wallet", large, map[string]string{
		billing.FieldChannelID: "31", "route_pool_id": "71", "procurement_cost_multiplier_ppm": "250000",
		billing.FieldMarketGross: "9999999999999999", billing.FieldTimestamp: "1791374400123",
	})
	second := economicsBatchEvent(t, sub, "batch-subscription", 123, map[string]string{
		billing.FieldChannelID: "32", "route_pool_id": "72", "subscription_id": "12",
		"procurement_cost_multiplier_ppm": "550000", billing.FieldTimestamp: "1791374400456",
	})
	zero := economicsBatchEvent(t, sub, "batch-zero", 0, map[string]string{
		billing.FieldChannelID: "0", "revenue_multiplier_ppm": "123456",
	})
	// A missing timestamp keeps the existing stream-ID fallback, including zero
	// monetary usage snapshots. Model-less financial events get no snapshot.
	zero.streamID = "1791374400789-1"
	modelLess := economicsBatchEvent(t, wallet, "batch-model-less", 7, map[string]string{billing.FieldModel: ""})
	batch := []event{first, second, zero, modelLess}
	result, err := post(ctx, pool, batch, nil)
	if err != nil || result.posted != 4 {
		t.Fatalf("post batch=%+v %v", result, err)
	}
	for _, want := range []struct {
		request                                             string
		channel, route, actual, subscription, cost, revenue int64
		source                                              string
		ms                                                  int64
	}{
		{"batch-wallet", 31, 71, large, 0, 250000, 700000, "wallet", 1791374400123},
		{"batch-subscription", 32, 72, 123, 12, 550000, 300000, "subscription", 1791374400456},
		{"batch-zero", 0, 0, 0, 0, 0, 123456, "subscription", 1791374400789},
	} {
		var channel, route, actual, subscription, cost, revenue int64
		var source string
		var settled time.Time
		if err := pool.QueryRow(ctx, `SELECT channel_id,route_pool_id,actual_amount,subscription_id,
		 procurement_cost_multiplier_ppm,revenue_multiplier_ppm,billing_source,settled_at
		 FROM v3_billing.request_economics WHERE request_id=$1`, want.request).
			Scan(&channel, &route, &actual, &subscription, &cost, &revenue, &source, &settled); err != nil {
			t.Fatal(err)
		}
		if channel != want.channel || route != want.route || actual != want.actual || subscription != want.subscription || cost != want.cost || revenue != want.revenue || source != want.source || settled.UnixMilli() != want.ms {
			t.Fatalf("snapshot %s=%d/%d/%d/%d/%d/%d/%s/%d, want %+v", want.request, channel, route, actual, subscription, cost, revenue, source, settled.UnixMilli(), want)
		}
	}
	if rows := count(t, pool, "request_economics"); rows != 3 {
		t.Fatalf("snapshot count=%d, want 3", rows)
	}
	if balance, version := pgBalance(t, pool, wallet); balance != 0 || version != 2 {
		t.Fatalf("wallet balance/version=%d/%d, want 0/2", balance, version)
	}
	if balance, version := pgBalance(t, pool, sub); balance != 877 || version != 1 {
		t.Fatalf("subscription balance/version=%d/%d, want 877/1", balance, version)
	}
	result, err = post(ctx, pool, batch, nil)
	if err != nil || result.duplicates != 4 || result.posted != 0 {
		t.Fatalf("batch redelivery=%+v %v", result, err)
	}
	var statements int
	if err := pool.QueryRow(ctx, `SELECT n FROM v3_billing.economics_insert_statements`).Scan(&statements); err != nil || statements != 1 {
		t.Fatalf("economics insert statements=%d %v, want 1 including replay", statements, err)
	}
	if count(t, pool, "ledger_entries") != 3 || count(t, pool, "request_economics") != 3 || count(t, pool, "usage_logs") != 3 {
		t.Fatal("batch redelivery duplicated financial or usage rows")
	}
}

func TestEconomicsBatchMixedFundingHasOneRootSnapshot(t *testing.T) {
	pool := testPool(t)
	sub, wallet := fundedAccount(t, pool, 7, 1000), fundedAccount(t, pool, 8, 1000)
	if _, err := pool.Exec(ctx, `UPDATE v3_billing.accounts SET owner_type='subscription',owner_id=1,kind='subscription' WHERE id=$1`, sub); err != nil {
		t.Fatal(err)
	}
	primary := economicsBatchEvent(t, sub, "batch-mixed", 60, map[string]string{
		"funding_part": "primary", "usage_total_amount": "100", billing.FieldBillingSource: "mixed",
		"subscription_id": "12", "funding_policy_version": "standard_v2", "funding_order_id": "21",
		"funding_wallet_equivalent_micro": "60", "funding_revenue_multiplier_ppm": "900000",
		"procurement_cost_multiplier_ppm": "250000", "revenue_multiplier_ppm": "0",
		billing.FieldTimestamp: "1791374400123",
	})
	secondary := economicsBatchWalletEvent(t, wallet, "batch-mixed", 40)
	secondary.fields["funding_part"], secondary.fields["usage_total_amount"] = "secondary", "100"
	secondary.fields[billing.FieldBillingSource] = "mixed"
	secondary.fingerprint = fingerprint(secondary.fields)
	if _, err := post(ctx, pool, []event{primary, secondary}, nil); err != nil {
		t.Fatal(err)
	}
	var actual, subscription, revenue int64
	var source string
	if err := pool.QueryRow(ctx, `SELECT actual_amount,billing_source,subscription_id,revenue_multiplier_ppm FROM v3_billing.request_economics WHERE request_id='batch-mixed'`).Scan(&actual, &source, &subscription, &revenue); err != nil || actual != 100 || source != "mixed" || subscription != 12 || revenue != 0 {
		t.Fatalf("root snapshot=%d/%s/%d/%d %v", actual, source, subscription, revenue, err)
	}
	if count(t, pool, "request_economics") != 1 || count(t, pool, "funding_source_usage") != 2 || count(t, pool, "ledger_entries") != 2 || count(t, pool, "usage_logs") != 1 {
		t.Fatal("mixed funding did not preserve one root and both bucket facts/debits")
	}
}

func TestEconomicsBatchHistoricalCollisionRollsBackEveryRequest(t *testing.T) {
	pool := testPool(t)
	account := fundedAccount(t, pool, 7, 1000)
	if _, err := pool.Exec(ctx, `INSERT INTO v3_billing.request_economics
	 (request_id,channel_id,route_pool_id,actual_amount,billing_source,subscription_id,procurement_cost_multiplier_ppm,revenue_multiplier_ppm,settled_at)
	 VALUES('batch-retained',9,8,777,'wallet',0,6,5,'2026-10-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	first := economicsBatchWalletEvent(t, account, "batch-fresh", 20)
	collision := economicsBatchWalletEvent(t, account, "batch-retained", 30)
	if _, err := post(ctx, pool, []event{first, collision}, nil); !errors.Is(err, billing.ErrPostConflict) {
		t.Fatalf("historical collision=%v", err)
	}
	assertEconomicsBatchRolledBack(t, pool, 1, account)
	var actual int64
	if err := pool.QueryRow(ctx, `SELECT actual_amount FROM v3_billing.request_economics WHERE request_id='batch-retained'`).Scan(&actual); err != nil || actual != 777 {
		t.Fatalf("historical snapshot changed=%d %v", actual, err)
	}
}

func TestEconomicsBatchSameRequestRootsRollBack(t *testing.T) {
	pool := testPool(t)
	firstAccount, secondAccount := fundedAccount(t, pool, 7, 1000), fundedAccount(t, pool, 8, 1000)
	first := economicsBatchWalletEvent(t, firstAccount, "batch-colliding-roots", 20)
	second := economicsBatchWalletEvent(t, secondAccount, "batch-colliding-roots", 30)
	if _, err := post(ctx, pool, []event{first, second}, nil); !errors.Is(err, billing.ErrPostConflict) {
		t.Fatalf("same-request root collision=%v", err)
	}
	assertEconomicsBatchRolledBack(t, pool, 0, firstAccount, secondAccount)
}

func TestEconomicsBatchMalformedSnapshotRollsBackEveryRequest(t *testing.T) {
	for _, invalid := range []struct{ field, value string }{
		{"route_pool_id", "bad"},
		{"revenue_multiplier_ppm", "-1"},
		{"revenue_multiplier_ppm", "9223372036854775808"},
		{billing.FieldBillingSource, "unknown"},
		{"usage_total_amount", "-1"},
	} {
		t.Run(invalid.field+"/"+invalid.value, func(t *testing.T) {
			pool := testPool(t)
			account := fundedAccount(t, pool, 7, 1000)
			first := economicsBatchWalletEvent(t, account, "batch-valid-before-malformed", 20)
			bad := economicsBatchWalletEvent(t, account, "batch-malformed", 30)
			bad.fields[invalid.field] = invalid.value
			if invalid.field == "usage_total_amount" {
				bad.fields["funding_part"] = "primary"
			}
			bad.fingerprint = fingerprint(bad.fields)
			_, err := post(ctx, pool, []event{first, bad}, nil)
			if err == nil || !strings.Contains(err.Error(), "invalid frozen economics") {
				t.Fatalf("malformed snapshot accepted or wrong error=%v", err)
			}
			assertEconomicsBatchRolledBack(t, pool, 0, account)
		})
	}
}
