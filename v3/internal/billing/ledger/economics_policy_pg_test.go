//go:build pgintegration

package ledger

import (
	"strconv"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing"
)

func TestSubscriptionEconomicsPreservesActualPolicyThroughRedelivery(t *testing.T) {
	pool := testPool(t)
	account := fundedAccount(t, pool, 7, 1000)
	if _, err := pool.Exec(ctx, `UPDATE v3_billing.accounts SET owner_type='subscription',owner_id=1,kind='subscription' WHERE id=$1`, account); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO v3_billing.funding_source_policies(source,revenue_multiplier_ppm) VALUES('subscription',300000)`); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	e := usageHookEvent(t, account, "sub-economic-policy", 100, false)
	e.fields[billing.FieldBillingSource] = "subscription"
	e.fields[billing.FieldProcurementCost] = "250000"
	e.fields[billing.FieldTimestamp] = strconv.FormatInt(now.UnixMilli(), 10)
	e.fingerprint = fingerprint(e.fields)
	if _, err := post(ctx, pool, []event{e}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_billing.funding_source_policies SET revenue_multiplier_ppm=900000 WHERE source='subscription'`); err != nil {
		t.Fatal(err)
	}
	if result, err := post(ctx, pool, []event{e}, nil); err != nil || result.duplicates != 1 {
		t.Fatalf("redelivery=%+v %v", result, err)
	}
	var ppm int64
	if err := pool.QueryRow(ctx, `SELECT revenue_multiplier_ppm FROM v3_billing.request_economics WHERE request_id='sub-economic-policy'`).Scan(&ppm); err != nil || ppm != 300000 {
		t.Fatalf("frozen source policy=%d %v", ppm, err)
	}
	report, err := DailyFundingEconomics(ctx, pool, now)
	if err != nil || report.RecognizedRevenue != 30 || report.RecognizedCost != 25 || report.RecognizedProfit != 5 || len(report.Sources) != 1 {
		t.Fatalf("subscription source report=%+v %v", report, err)
	}
	if balance, version := pgBalance(t, pool, account); balance != 900 || version != 1 {
		t.Fatalf("policy replay changed money=%d/%d", balance, version)
	}
}
