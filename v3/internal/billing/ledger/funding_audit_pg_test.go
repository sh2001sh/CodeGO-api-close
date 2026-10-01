//go:build pgintegration

package ledger

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func TestFundingAuditImportedFIFOAndReplay(t *testing.T) {
	pool := testPool(t)
	account := fundedAccount(t, pool, 7, 300)
	if _, err := pool.Exec(ctx, `INSERT INTO v3_billing.funding_lots
	 (lot_id,source_account_id,account_id,source,idempotency_key,original_amount,remaining_amount,revenue_multiplier_ppm,created_at)
	 VALUES('imported-lot','source-wallet',$1,'blind_box','imported-key',400,300,900000,now()-interval '1 day');
	 INSERT INTO v3_billing.funding_source_policies VALUES(700000,now(),'topup')`, pgx.QueryExecModeSimpleProtocol, account); err != nil {
		t.Fatal(err)
	}
	p := NewPoster(pool)
	credit := billing.Entry{AccountID: account, Amount: 500, Kind: "topup", OperationID: "payment"}
	for range 2 {
		if _, err := p.Post(ctx, credit); err != nil {
			t.Fatal(err)
		}
	}
	if bal, _ := pgBalance(t, pool, account); bal != 800 {
		t.Fatalf("imported money credited again: %d", bal)
	}
	e := usageHookEvent(t, account, "fifo", 450, false)
	e.fields["procurement_cost_multiplier_ppm"], e.fields["route_pool_id"] = "250000", "77"
	e.fields[billing.FieldMarketGross] = "9999"
	e.fields[billing.FieldTimestamp] = strconv.FormatInt(time.Now().UnixMilli(), 10)
	e.fingerprint = fingerprint(e.fields)
	if _, err := post(ctx, pool, []event{e}, nil); err != nil {
		t.Fatal(err)
	}
	if result, err := post(ctx, pool, []event{e}, nil); err != nil || result.duplicates != 1 {
		t.Fatalf("replay=%+v %v", result, err)
	}
	var imported, native, remaining, actual, ppm, route int64
	if err := pool.QueryRow(ctx, `SELECT
	 (SELECT amount FROM v3_billing.funding_allocations WHERE lot_id='imported-lot'),
	 (SELECT amount FROM v3_billing.funding_allocations WHERE source='topup'),
	 (SELECT remaining_amount FROM v3_billing.funding_lots WHERE source='topup'),
	 actual_amount,revenue_multiplier_ppm,route_pool_id FROM v3_billing.request_economics WHERE request_id='fifo'`).Scan(&imported, &native, &remaining, &actual, &ppm, &route); err != nil {
		t.Fatal(err)
	}
	if imported != 300 || native != 150 || remaining != 350 || actual != 450 || ppm != 833333 || route != 77 {
		t.Fatalf("FIFO/import/economics=%d/%d/%d/%d/%d/%d", imported, native, remaining, actual, ppm, route)
	}
	if n := count(t, pool, "funding_lots"); n != 2 {
		t.Fatalf("duplicated lots: %d", n)
	}
	report, err := DailyFundingEconomics(ctx, pool, time.Now())
	if err != nil || report.RecognizedRevenue != 375 || report.RecognizedCost != 113 || report.RecognizedProfit != 262 || len(report.Sources) != 2 {
		t.Fatalf("native daily source report=%+v %v", report, err)
	}
}

func TestFundingAuditConcurrentSpendAndBusinessRollback(t *testing.T) {
	pool := testPool(t)
	account := fundedAccount(t, pool, 7, 0)
	p := NewPoster(pool)
	if _, err := p.Post(ctx, billing.Entry{AccountID: account, Amount: 1000, Kind: "topup", OperationID: "credit"}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			e := billing.Entry{AccountID: account, Amount: -50, Kind: "usage", OperationID: "spend:" + strconv.Itoa(i)}
			for range 2 {
				if _, err := p.Post(ctx, e); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
	if bal, _ := pgBalance(t, pool, account); bal != 0 {
		t.Fatalf("parallel balance=%d", bal)
	}
	var allocated, remaining int64
	if err := pool.QueryRow(ctx, `SELECT (SELECT sum(amount) FROM v3_billing.funding_allocations),(SELECT sum(remaining_amount) FROM v3_billing.funding_lots)`).Scan(&allocated, &remaining); err != nil || allocated != 1000 || remaining != 0 {
		t.Fatalf("FIFO amount=%d/%d %v", allocated, remaining, err)
	}
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := p.PostTx(ctx, tx, billing.Entry{AccountID: account, Amount: 500, Kind: "topup", OperationID: "rollback-credit"}); err != nil {
			return err
		}
		if _, err := p.PostTx(ctx, tx, billing.Entry{AccountID: account, Amount: -200, Kind: "usage", OperationID: "rollback-debit"}); err != nil {
			return err
		}
		return errors.New("business state failed after posting")
	})
	if err == nil {
		t.Fatal("failure not propagated")
	}
	if bal, _ := pgBalance(t, pool, account); bal != 0 || count(t, pool, "funding_lots") != 1 || count(t, pool, "funding_allocations") != 20 {
		t.Fatal("rollback committed provenance or money")
	}
}

func TestFundingAuditFailureRollsBackDedupAndExactBigint(t *testing.T) {
	pool := testPool(t)
	account := fundedAccount(t, pool, 7, 0)
	p := NewPoster(pool)
	const large = int64(9007199254740993)
	if _, err := p.Post(ctx, billing.Entry{AccountID: account, Amount: credits.Micro(large), Kind: "topup", OperationID: "large"}); err != nil {
		t.Fatal(err)
	}
	e := usageHookEvent(t, account, "invalid-economics", 1, false)
	e.fields["procurement_cost_multiplier_ppm"] = "9223372036854775808"
	e.fingerprint = fingerprint(e.fields)
	if _, err := post(ctx, pool, []event{e}, nil); err == nil {
		t.Fatal("overflow economics accepted")
	}
	if bal, _ := pgBalance(t, pool, account); bal != large || count(t, pool, "billing_dedup") != 0 || count(t, pool, "funding_allocations") != 0 {
		t.Fatal("failed usage leaked writes")
	}
	e.fields["procurement_cost_multiplier_ppm"] = "0"
	e.amount, e.fields[billing.FieldAmount] = large, strconv.FormatInt(large, 10)
	e.fingerprint = fingerprint(e.fields)
	if _, err := post(ctx, pool, []event{e}, nil); err != nil {
		t.Fatal(err)
	}
	var got int64
	if err := pool.QueryRow(ctx, `SELECT amount FROM v3_billing.funding_allocations`).Scan(&got); err != nil || got != large {
		t.Fatalf("exact amount=%d %v", got, err)
	}
	if _, err := p.Post(ctx, billing.Entry{AccountID: account, Amount: credits.Micro(math.MaxInt64), Kind: "topup", OperationID: "max"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Post(ctx, billing.Entry{AccountID: account, Amount: 1, Kind: "topup", OperationID: "overflow"}); !errors.Is(err, credits.ErrOverflow) {
		t.Fatalf("money overflow=%v", err)
	}
}

func TestFundingAuditHistoricalCollisionRejectsFreshDebit(t *testing.T) {
	pool := testPool(t)
	account := fundedAccount(t, pool, 7, 1000)
	if _, err := pool.Exec(ctx, `INSERT INTO v3_billing.request_economics VALUES(3,0,100,0,0,0,$1,$1,'retained-request','wallet')`, time.Now()); err != nil {
		t.Fatal(err)
	}
	e := usageHookEvent(t, account, "retained-request", 100, false)
	if _, err := post(ctx, pool, []event{e}, nil); !errors.Is(err, billing.ErrPostConflict) {
		t.Fatalf("historical collision=%v", err)
	}
	if bal, _ := pgBalance(t, pool, account); bal != 1000 || count(t, pool, "funding_allocations") != 0 || count(t, pool, "billing_dedup") != 0 {
		t.Fatal("historical replay changed balance")
	}
}

func TestFundingAuditImportedLotSumOverflowFailsClosed(t *testing.T) {
	pool := testPool(t)
	account := fundedAccount(t, pool, 7, 1000)
	for i := range 2 {
		if _, err := pool.Exec(ctx, `INSERT INTO v3_billing.funding_lots
		 (lot_id,source_account_id,account_id,source,idempotency_key,original_amount,remaining_amount,revenue_multiplier_ppm)
		 VALUES($1,'source-wallet',$2,'topup',$1,$3,$3,1000000)`, fmt.Sprintf("large-import-%d", i), account, int64(math.MaxInt64)); err != nil {
			t.Fatal(err)
		}
	}
	e := usageHookEvent(t, account, "overflow-lots", 1, false)
	if _, err := post(ctx, pool, []event{e}, nil); !errors.Is(err, credits.ErrOverflow) {
		t.Fatalf("imported lot overflow=%v", err)
	}
	if bal, _ := pgBalance(t, pool, account); bal != 1000 || count(t, pool, "billing_dedup") != 0 || count(t, pool, "funding_allocations") != 0 {
		t.Fatal("overflowed imported funding partially posted")
	}
}
