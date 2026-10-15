//go:build pgintegration

package ledger

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func TestOwnerFundingPrefixMatchesFullFIFOAcrossPagesAndDeficits(t *testing.T) {
	pool := testPool(t)
	var appendAccount int64
	for i, amount := range []credits.Micro{1, 65, 129, 200, 350} {
		t.Run(fmt.Sprint(int64(amount)), func(t *testing.T) {
			account := fundedAccount(t, pool, int64(100+i*2), 300)
			if i == 0 {
				appendAccount = account
			}
			reference := fundedAccount(t, pool, int64(101+i*2), 300)
			for _, id := range []int64{account, reference} {
				if _, err := pool.Exec(ctx, `INSERT INTO v3_billing.funding_lots
				 (lot_id,source_account_id,account_id,source,reference_type,reference_id,idempotency_key,original_amount,remaining_amount,revenue_multiplier_ppm,created_at,non_transferable,non_refundable)
				 SELECT ($1::bigint)::text||':'||lpad(n::text,4,'0'),'fixture',$1,
				 CASE n%3 WHEN 0 THEN 'topup' WHEN 1 THEN 'blind_box' ELSE 'legacy_unattributed' END,
				 'fixture',n::text,($1::bigint)::text||':'||n,1,1,n*1000,'2026-10-01'::timestamptz+make_interval(secs=>n/5),n%2=0,n%2=1
				 FROM generate_series(1,150) n`, id); err != nil {
					t.Fatal(err)
				}
			}
			now := time.Now()
			if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
				if _, err := tx.Exec(ctx, `SELECT id FROM v3_billing.accounts WHERE id=ANY($1) ORDER BY id FOR UPDATE`, []int64{account, reference}); err != nil {
					return err
				}
				prefix, known, err := lockOwnerFundingPrefixTx(ctx, tx, account, amount)
				if err != nil {
					return err
				}
				want := 150
				if amount <= 64 {
					want = 64
				} else if amount <= 128 {
					want = 128
				}
				if len(prefix) != want || int64(known) != int64(want) {
					t.Fatalf("read %d lots total=%d, want %d", len(prefix), known, want)
				}
				// Compare with the previous full-read implementation, including
				// its exact paid-versus-overdraft backfill and source ordering.
				lots, total, err := lockFundingLotsTx(ctx, tx, reference, "")
				if err != nil {
					return err
				}
				if deficit := amount - total; deficit > 0 {
					lots, err = backfillUnattributedLotsTx(ctx, tx, reference, "reference", deficit, 300, total, lots, now)
					if err != nil {
						return err
					}
				}
				if err := consumeFundingModeLotsTx(ctx, tx, reference, "reference", amount, lots, now, fundingOwnerSpend); err != nil {
					return err
				}
				return allocateFundingTx(ctx, tx, account, "prefix", amount, 300, now)
			}); err != nil {
				t.Fatal(err)
			}
			facts := func(id int64) []string {
				rows, err := pool.Query(ctx, `SELECT concat_ws(':',l.source,l.reference_type,
				 CASE WHEN l.reference_type='fixture' THEN l.reference_id ELSE '' END,a.amount,l.remaining_amount,l.revenue_multiplier_ppm)
				 FROM v3_billing.funding_allocations a JOIN v3_billing.funding_lots l USING(lot_id)
				 WHERE a.account_id=$1 ORDER BY 1`, id)
				if err != nil {
					t.Fatal(err)
				}
				defer rows.Close()
				var out []string
				for rows.Next() {
					var fact string
					if err := rows.Scan(&fact); err != nil {
						t.Fatal(err)
					}
					out = append(out, fact)
				}
				if err := rows.Err(); err != nil {
					t.Fatal(err)
				}
				return out
			}
			if got, want := facts(account), facts(reference); !reflect.DeepEqual(got, want) {
				t.Fatalf("prefix attribution differs from full FIFO:\ngot=%v\nwant=%v", got, want)
			}
		})
	}
	// Each charge restarts FIFO from the first live lot. An origin credited
	// after an earlier charge, even with an older timestamp, cannot be skipped.
	if _, err := pool.Exec(ctx, `INSERT INTO v3_billing.funding_lots
	 (lot_id,source_account_id,account_id,source,idempotency_key,original_amount,remaining_amount,revenue_multiplier_ppm,created_at)
	 VALUES('appended-old-origin','fixture',$1,'topup','appended-old-origin',10,10,700000,'2026-09-30')`, appendAccount); err != nil {
		t.Fatal(err)
	}
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT id FROM v3_billing.accounts WHERE id=$1 FOR UPDATE`, appendAccount); err != nil {
			return err
		}
		return allocateFundingTx(ctx, tx, appendAccount, "appended", 5, 300, time.Now())
	}); err != nil {
		t.Fatal(err)
	}
	var appendedAmount, appendedRemaining int64
	if err := pool.QueryRow(ctx, `SELECT a.amount,l.remaining_amount FROM v3_billing.funding_allocations a
	 JOIN v3_billing.funding_lots l USING(lot_id) WHERE a.request_id='appended'`).Scan(&appendedAmount, &appendedRemaining); err != nil || appendedAmount != 5 || appendedRemaining != 5 {
		t.Fatalf("appended origin amount/remaining=%d/%d err=%v", appendedAmount, appendedRemaining, err)
	}
	wideAccount := fundedAccount(t, pool, 999, 30000)
	if _, err := pool.Exec(ctx, `INSERT INTO v3_billing.funding_lots
	 (lot_id,source_account_id,account_id,source,idempotency_key,original_amount,remaining_amount,revenue_multiplier_ppm,created_at)
	 SELECT 'wide:'||lpad(n::text,6,'0'),'fixture',$1,'topup','wide:'||n,1,1,1000000,
	 '2026-10-01'::timestamptz+make_interval(secs=>n) FROM generate_series(1,30000) n`, wideAccount); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ANALYZE v3_billing.funding_lots`); err != nil {
		t.Fatal(err)
	}
	// The seek condition must remain an index bound, even when timestamps tie.
	rows, err := pool.Query(ctx, `EXPLAIN (COSTS OFF) SELECT lot_id,created_at FROM v3_billing.funding_lots
	 WHERE account_id=$1 AND remaining_amount>0 AND (created_at,lot_id)>('2026-10-01'::timestamptz,'wide:000001')
	 ORDER BY created_at,lot_id LIMIT 64 FOR UPDATE`, wideAccount)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(plan, "\n")
	t.Logf("FIFO seek plan:\n%s", joined)
	if !strings.Contains(joined, "Index Scan using funding_lots_fifo_idx") || strings.Contains(joined, "Sort") {
		t.Fatalf("FIFO seek did not use its index:\n%s", joined)
	}
}
