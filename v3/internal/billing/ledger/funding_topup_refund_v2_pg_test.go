//go:build pgintegration

package ledger

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func providerTopupRefundEntryV2(account int64, trade string, amount credits.Micro) billing.Entry {
	return billing.Entry{AccountID: account, Amount: -amount, Kind: "adjustment", OperationID: "order:refund:" + trade + ":confirmed",
		Reason: "provider confirmed cumulative refund", Metadata: map[string]any{"refund_trade_no": trade}}
}

func TestFundingTopupRefundV2LaterOrderPreservesOlderPaymentAndConversion(t *testing.T) {
	pool := testPool(t)
	account := fundedAccount(t, pool, 7, 0)
	p := NewPoster(pool)
	rewardPost(t, p, account, 100, "topup", "", "order:paid:earlier")
	seedConversionV2(t, p, account, 50, 0)
	rewardPost(t, p, account, 200, "topup", "", "order:paid:later")
	e := providerTopupRefundEntryV2(account, "later", 20)
	entries, outbox := count(t, pool, "ledger_entries"), count(t, pool, "balance_outbox")
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := p.PostTx(ctx, tx, e); err != nil {
			return err
		}
		return errors.New("provider business transition failed")
	})
	if err == nil || count(t, pool, "ledger_entries") != entries || count(t, pool, "balance_outbox") != outbox || count(t, pool, "funding_allocations") != 0 {
		t.Fatal("provider transition failure committed money or source audit")
	}
	for range 2 {
		fundingV2Post(t, p, e)
	}
	var earlier, later, converted int64
	if err := pool.QueryRow(ctx, `SELECT
	 (SELECT remaining_amount FROM v3_billing.funding_lots WHERE reference_id='order:paid:earlier'),
	 (SELECT remaining_amount FROM v3_billing.funding_lots WHERE reference_id='order:paid:later'),
	 (SELECT remaining_amount FROM v3_billing.funding_lots WHERE source='subscription_conversion')`).Scan(&earlier, &later, &converted); err != nil || earlier != 100 || later != 180 || converted != 50 {
		t.Fatalf("targeted provider sources=%d/%d/%d err=%v", earlier, later, converted, err)
	}
	if balance, _ := pgBalance(t, pool, account); balance != 330 || count(t, pool, "funding_allocations") != 1 {
		t.Fatalf("provider replay conservation balance=%d", balance)
	}
	rewardPost(t, p, account, -10, "usage", "", "ordinary-after-provider-refund")
	if err := pool.QueryRow(ctx, `SELECT remaining_amount FROM v3_billing.funding_lots WHERE reference_id='order:paid:earlier'`).Scan(&earlier); err != nil || earlier != 90 {
		t.Fatalf("ordinary FIFO changed after provider priority: %d %v", earlier, err)
	}
}

func TestFundingTopupRefundV2SpentChargebackPreservesExplicitDebt(t *testing.T) {
	for _, extra := range []credits.Micro{0, 20} {
		t.Run(fmt.Sprint(extra), func(t *testing.T) {
			pool := testPool(t)
			account := fundedAccount(t, pool, 7, 0)
			p := NewPoster(pool)
			rewardPost(t, p, account, 50, "topup", "", "order:paid:spent")
			rewardPost(t, p, account, -40, "usage", "", "spent-original")
			if extra > 0 {
				rewardPost(t, p, account, extra, "topup", "", "order:paid:unrelated-after-spent")
			}
			e := providerTopupRefundEntryV2(account, "spent", 50)
			for range 2 {
				fundingV2Post(t, p, e)
			}
			if balance, _ := pgBalance(t, pool, account); balance != int64(extra-40) {
				t.Fatalf("verified loss omitted or duplicated: balance=%d", balance)
			}
			var origin, fallback, debt int64
			if err := pool.QueryRow(ctx, `SELECT
			 coalesce(sum(a.amount) FILTER(WHERE l.reference_id='order:paid:spent'),0)::bigint,
			 coalesce(sum(a.amount) FILTER(WHERE l.reference_id='order:paid:unrelated-after-spent'),0)::bigint,
			 coalesce(sum(a.amount) FILTER(WHERE l.reference_type='gateway_overdraft'),0)::bigint
			 FROM v3_billing.funding_allocations a JOIN v3_billing.funding_lots l USING(lot_id)
			 WHERE a.request_id=$1`, "native:operation:"+e.OperationID).Scan(&origin, &fallback, &debt); err != nil || origin != 10 || fallback != int64(extra) || debt != int64(40-extra) {
				t.Fatalf("verified loss origin/fallback/debt=%d/%d/%d err=%v", origin, fallback, debt, err)
			}
		})
	}
}

func TestFundingTopupRefundV2OwnerReservesAndRestoresOwnTopupBeforeConversion(t *testing.T) {
	pool := testPool(t)
	account := fundedAccount(t, pool, 7, 0)
	p := NewPoster(pool)
	seedConversionV2(t, p, account, 100, 20)
	rewardPost(t, p, account, 50, "topup", "", "order:paid:owner-topup")
	meta := map[string]any{"refund_trade_no": "owner-topup", "refund_no": "owner-v2"}
	e := billing.Entry{AccountID: account, Amount: -40, Kind: "refund", OperationID: "user-refund:owner-v2:reserve", Metadata: meta}
	for range 2 {
		fundingV2Post(t, p, e)
	}
	var topup, conversion, allocated int64
	if err := pool.QueryRow(ctx, `SELECT
	 (SELECT remaining_amount FROM v3_billing.funding_lots WHERE reference_id='order:paid:owner-topup'),
	 (SELECT sum(remaining_amount)::bigint FROM v3_billing.funding_lots WHERE source='subscription_conversion'),
	 (SELECT sum(a.amount)::bigint FROM v3_billing.funding_allocations a JOIN v3_billing.funding_lots l USING(lot_id)
	 WHERE a.request_id=$1 AND l.reference_id='order:paid:owner-topup')`, "native:operation:"+e.OperationID).Scan(&topup, &conversion, &allocated); err != nil || topup != 10 || conversion != 120 || allocated != 40 {
		t.Fatalf("owner refund origin=%d/%d/%d err=%v", topup, conversion, allocated, err)
	}
	release := billing.Entry{AccountID: account, Amount: 40, Kind: "refund", OperationID: "user-refund:owner-v2:release", Metadata: meta}
	for range 2 {
		fundingV2Post(t, p, release)
	}
	if balance, _ := pgBalance(t, pool, account); balance != 170 {
		t.Fatalf("owner failed refund restoration=%d", balance)
	}
	if err := pool.QueryRow(ctx, `SELECT remaining_amount FROM v3_billing.funding_lots WHERE reference_id='order:paid:owner-topup'`).Scan(&topup); err != nil || topup != 50 || count(t, pool, "funding_lots") != 3 {
		t.Fatalf("owner failed refund restored wrong origin=%d err=%v", topup, err)
	}
}
