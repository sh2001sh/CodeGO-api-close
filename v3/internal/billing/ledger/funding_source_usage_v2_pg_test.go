//go:build pgintegration

package ledger

import (
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
)

func TestPerBucketEconomicsCannotUseGlobalLegacyFactor(t *testing.T) {
	pool := testPool(t)
	sub := fundedAccount(t, pool, 7, 1000)
	wallet := fundedAccount(t, pool, 8, 0)
	if _, err := pool.Exec(ctx, `UPDATE v3_billing.accounts SET owner_type='subscription',owner_id=1,kind='subscription' WHERE id=$1`, sub); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO v3_billing.funding_source_policies(source,revenue_multiplier_ppm) VALUES('topup',1000000),('subscription',9000000)`); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPoster(pool).Post(ctx, billing.Entry{AccountID: wallet, Amount: 1000, Kind: "topup", OperationID: "paid"}); err != nil {
		t.Fatal(err)
	}
	first := usageHookEvent(t, sub, "native-mixed", 60, false)
	second := usageHookEvent(t, wallet, "native-mixed", 40, false)
	for index, e := range []*event{&first, &second} {
		e.fields[billing.FieldTimestamp] = strconv.FormatInt(time.Now().UnixMilli(), 10)
		e.fields["funding_policy_version"] = "wallet"
		e.fields["funding_wallet_equivalent_micro"] = e.fields[billing.FieldAmount]
		e.fields["funding_procurement_cost_micro"] = "20"
		e.fields["funding_part"] = "secondary"
		e.fields["usage_total_amount"] = "100"
		e.fields[billing.FieldBillingSource] = "mixed"
		if index == 0 {
			e.fields["funding_part"] = "primary"
			e.fields["funding_policy_version"] = "standard_v2"
			e.fields["funding_order_id"] = "21"
			e.fields["funding_revenue_multiplier_ppm"] = "900000"
			e.fields["funding_procurement_cost_micro"] = "30"
		}
		e.fingerprint = fingerprint(e.fields)
	}
	for range 2 {
		if _, err := post(ctx, pool, []event{first, second}, nil); err != nil {
			t.Fatal(err)
		}
	}
	var count int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_billing.funding_source_usage`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("immutable facts=%d %v", count, err)
	}
	report, err := DailyFundingEconomics(ctx, pool, time.Now())
	if err != nil || report.RecognizedRevenue != 94 || report.RecognizedCost != 50 || report.RecognizedProfit != 44 || report.UnpricedRequests != 0 {
		t.Fatalf("facts report=%+v %v", report, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_billing.funding_source_policies SET revenue_multiplier_ppm=0`); err != nil {
		t.Fatal(err)
	}
	replay, err := DailyFundingEconomics(ctx, pool, time.Now())
	if err != nil || replay.RecognizedRevenue != 94 {
		t.Fatalf("policy mutated facts=%+v %v", replay, err)
	}
}

func TestUnknownSourceCostRemainsNullAndMalformedFactsRollback(t *testing.T) {
	pool := testPool(t)
	account := fundedAccount(t, pool, 7, 1000)
	e := usageHookEvent(t, account, "cost-unknown", 20, false)
	e.fields[billing.FieldTimestamp] = strconv.FormatInt(time.Now().UnixMilli(), 10)
	e.fields["funding_policy_version"] = "wallet"
	e.fields["funding_wallet_equivalent_micro"] = "20"
	e.fingerprint = fingerprint(e.fields)
	if _, err := post(ctx, pool, []event{e}, nil); err != nil {
		t.Fatal(err)
	}
	var cost *int64
	if err := pool.QueryRow(ctx, `SELECT procurement_cost_amount FROM v3_billing.funding_source_usage`).Scan(&cost); err != nil || cost != nil {
		t.Fatalf("unknown cost=%v %v", cost, err)
	}
	report, err := DailyFundingEconomics(ctx, pool, time.Now())
	if err != nil || report.UnpricedRequests != 1 {
		t.Fatalf("review=%+v %v", report, err)
	}
	bad := usageHookEvent(t, account, "cost-malformed", 20, false)
	bad.fields["funding_policy_version"] = "standard_v2"
	bad.fields["funding_wallet_equivalent_micro"] = "-1"
	bad.fingerprint = fingerprint(bad.fields)
	if _, err := post(ctx, pool, []event{bad}, nil); err == nil {
		t.Fatal("negative share accepted")
	}
	if balance, _ := pgBalance(t, pool, account); balance != 980 {
		t.Fatalf("failed economics changed balance=%d", balance)
	}
	if err := pool.QueryRow(ctx, `SELECT 1 FROM v3_billing.billing_dedup WHERE request_id='cost-malformed'`).Scan(new(int)); err != pgx.ErrNoRows {
		t.Fatalf("failure committed dedup=%v", err)
	}
}
