//go:build pgintegration

package ledger

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func TestFundingRestrictionBalancesAndPlans(t *testing.T) {
	pool := testPool(t)
	account := fundedAccount(t, pool, 7, 1000)
	empty := fundedAccount(t, pool, 8, 1000)
	// Many exhausted historical lots and unrelated accounts must not make a
	// wallet restriction read scale with the entire provenance archive.
	if _, err := pool.Exec(ctx, `INSERT INTO v3_billing.funding_lots
	 (lot_id,source_account_id,account_id,source,reference_type,reference_id,idempotency_key,original_amount,remaining_amount,revenue_multiplier_ppm,non_transferable,non_refundable)
	 SELECT 'history-'||n,'history',CASE WHEN n%2=0 THEN $1::bigint ELSE $2::bigint END,
	 'legacy_unattributed','opening','history-'||n,'history-'||n,100,0,0,true,true
	 FROM generate_series(1,30000) n`, account, empty); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO v3_billing.funding_lots
	 (lot_id,source_account_id,account_id,source,reference_type,reference_id,idempotency_key,original_amount,remaining_amount,revenue_multiplier_ppm,non_transferable,non_refundable)
	 VALUES ('transfer-lock','native',$1,'legacy_unattributed','opening','transfer','transfer',200,200,0,true,false),
	 ('refund-lock','native',$1,'legacy_unattributed','opening','refund','refund',300,300,0,false,true),
	 ('both-lock','native',$1,'legacy_unattributed','opening','both','both',100,100,0,true,true),
	 ('paid','native',$1,'topup','topup','paid','paid',400,400,1000000,false,false)`, account); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ANALYZE v3_billing.funding_lots`); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                     string
		id                       int64
		wantTransfer, wantRefund credits.Micro
	}{
		{"mixed restrictions", account, 700, 600},
		{"only exhausted restrictions", empty, 1000, 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
				transfer, err := TransferableBalanceTx(ctx, tx, tc.id, time.Now())
				if err != nil {
					return err
				}
				refund, err := RefundableBalanceTx(ctx, tx, tc.id)
				if err == nil && (transfer != tc.wantTransfer || refund != tc.wantRefund) {
					t.Errorf("transfer=%d refund=%d; want %d/%d", transfer, refund, tc.wantTransfer, tc.wantRefund)
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, restriction := range []string{"non_transferable", "non_refundable"} {
		rows, err := pool.Query(ctx, `EXPLAIN (COSTS OFF) SELECT coalesce(sum(remaining_amount),0)::bigint
		 FROM v3_billing.funding_lots WHERE account_id=$1 AND `+restriction+` AND remaining_amount>0`, account)
		if err != nil {
			t.Fatal(err)
		}
		var lines []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			lines = append(lines, line)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		plan := strings.Join(lines, "\n")
		t.Logf("%s plan:\n%s", restriction, plan)
		if !strings.Contains(plan, "funding_lots_active_restrictions_idx") || strings.Contains(plan, "Seq Scan") {
			t.Fatalf("restriction query did not choose the active restriction index:\n%s", plan)
		}
	}
}
