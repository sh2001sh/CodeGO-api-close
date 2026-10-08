//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func convertedRefundPackage(t *testing.T, s *commerce.Service, pool *pgxpool.Pool) (commerce.Order, commerce.Subscription) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO v3_catalog.groups(name) VALUES('default'),('vip') ON CONFLICT DO NOTHING; UPDATE v3_identity.users SET group_name='default' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	p, err := s.SavePlan(ctx, commerce.Plan{Name: "legacy paid", PriceMinor: 1000, Currency: "cny", Credits: 1000,
		DurationUnit: "day", DurationValue: 30, UpgradeGroup: "vip", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	o := paidRefundOrder(t, s, 1000, p.ID)
	sub := onlySubscription(t, s)
	q := configureConversion(t, s, sub.ID, 1000, 1000000, 800000, nil)
	if q.State != "quoted" {
		t.Fatalf("conversion not ready %+v", q)
	}
	if _, err = s.ConfirmWalletConversion(ctx, 1, q.QuoteID, "converted-refund-fixture", true); err != nil {
		t.Fatal(err)
	}
	return o, sub
}

func assertConvertedRefund(t *testing.T, pool *pgxpool.Pool, o commerce.Order, sub commerce.Subscription, wallet, exposure credits.Micro) {
	t.Helper()
	ctx := context.Background()
	var balance, converted, consumed int64
	var state, group string
	var benefits bool
	err := pool.QueryRow(ctx, `SELECT a.balance,o.state,s.benefits_until IS NOT NULL,u.group_name,
	 (SELECT coalesce(sum(remaining_amount),0)::bigint FROM v3_billing.funding_lots WHERE source='subscription_conversion'),
	 (SELECT consumed_credits FROM v3_billing.subscription_conversion_revocations WHERE original_order_id=$1)
	 FROM v3_commerce.orders o JOIN v3_commerce.subscriptions s ON s.order_id=o.id
	 JOIN v3_identity.users u ON u.id=o.user_id JOIN v3_billing.accounts a ON a.owner_type='user' AND a.owner_id=u.id AND a.kind='wallet'
	 WHERE o.id=$1 AND s.id=$2`, o.ID, sub.ID).Scan(&balance, &state, &benefits, &group, &converted, &consumed)
	if err != nil || balance != int64(wallet) || state != "refunded" || benefits || group != "default" || converted != 0 || consumed != int64(exposure) {
		t.Fatalf("refund wallet=%d state=%s benefits=%v group=%s conversion=%d exposure=%d err=%v", balance, state, benefits, group, converted, consumed, err)
	}
	reviews := 0
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.package_payment_reviews WHERE order_id=$1`, o.ID).Scan(&reviews); err != nil || (reviews > 0) != (exposure > 0) {
		t.Fatalf("consumed exposure review count=%d err=%v", reviews, err)
	}
}

func TestConvertedProviderRefundPreservesTopupAndReplays(t *testing.T) {
	for _, spent := range []credits.Micro{0, 400000} {
		t.Run(fmt.Sprint(spent), func(t *testing.T) {
			s, refunds, pool, _ := refundServices(t)
			ctx := context.Background()
			if spent == 0 {
				paidRefundOrder(t, s, 1000, 0) // older unrelated principal
			}
			o, sub := convertedRefundPackage(t, s, pool)
			account := refundAccount(t, pool)
			if spent > 0 {
				refundPost(t, pool, account, -spent, "usage", "converted-spent")
			}
			topup := paidRefundOrder(t, s, 1000, 0)
			for i := range 3 {
				if err := s.ConfirmRefundTotal(ctx, "epay", o.TradeNo, fmt.Sprintf("converted-refund-event-%d", i), "cny", o.AmountMinor); err != nil {
					t.Fatal(err)
				}
			}
			wallet := credits.Micro(10000000)
			if spent == 0 {
				wallet *= 2
			}
			assertConvertedRefund(t, pool, o, sub, wallet, spent)
			items, err := refunds.Eligible(ctx, 1)
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range items {
				if item.TradeNo == topup.TradeNo && (item.RemainingQuota != 10000000 || !item.Refundable) {
					t.Fatalf("unrelated later topup changed %+v", item)
				}
			}
		})
	}
}

func TestUserRefundReservesOnlyOriginalTopupDespiteOlderConversion(t *testing.T) {
	s, refunds, pool, provider := refundServices(t)
	ctx := context.Background()
	o, sub := convertedRefundPackage(t, s, pool)
	topup := paidRefundOrder(t, s, 1000, 0)
	r, err := refunds.Create(ctx, 1, commerce.UserRefundRequest{OrderType: "balance", TradeNo: topup.TradeNo})
	if err != nil || r.Status != "success" || r.AmountMinor != 980 {
		t.Fatalf("exact original topup refund %+v err=%v", r, err)
	}
	var balance, converted, reservedConverted int64
	if err = pool.QueryRow(ctx, `SELECT a.balance,
 (SELECT coalesce(sum(remaining_amount),0)::bigint FROM v3_billing.funding_lots WHERE source='subscription_conversion'),
 (SELECT coalesce(sum(amount),0)::bigint FROM v3_billing.funding_allocations WHERE request_id=$2 AND source='subscription_conversion')
 FROM v3_billing.accounts a WHERE a.id=$1`, refundAccount(t, pool), "native:operation:user-refund:"+r.RefundNo+":reserve").Scan(&balance, &converted, &reservedConverted); err != nil || balance != 1000000 || converted != 1000000 || reservedConverted != 0 || provider.creates != 1 {
		t.Fatalf("conversion funded topup refund wallet=%d conversion=%d reserved_conversion=%d provider=%d err=%v", balance, converted, reservedConverted, provider.creates, err)
	}
	if err = s.ConfirmRefundTotal(ctx, "epay", o.TradeNo, "converted-after-exact-topup-refund", "cny", o.AmountMinor); err != nil {
		t.Fatal(err)
	}
	assertConvertedRefund(t, pool, o, sub, 0, 0)
}

func TestConvertedProviderRefundFollowsPeerOrigin(t *testing.T) {
	s, _, pool, _ := refundServices(t)
	ctx := context.Background()
	o, sub := convertedRefundPackage(t, s, pool)
	poster := ledger.NewPoster(pool)
	sender := refundAccount(t, pool)
	var recipient int64
	if err := pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',2,'wallet') RETURNING id`).Scan(&recipient); err != nil {
		t.Fatal(err)
	}
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		_, err := poster.PostTx(ctx, tx, billing.Entry{AccountID: sender, Amount: -200000, Kind: "transfer", OperationID: "wallet-transfer:converted-peer:debit", Reason: "wallet_peer_transfer_debit", Metadata: map[string]any{"recipient_user_id": int64(2)}})
		if err != nil {
			return err
		}
		_, err = poster.PostTx(ctx, tx, billing.Entry{AccountID: recipient, Amount: 200000, Kind: "transfer", OperationID: "wallet-transfer:converted-peer:credit", Reason: "wallet_peer_transfer_credit", Metadata: map[string]any{"request_id": "converted-peer"}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	paidRefundOrder(t, s, 1000, 0)
	refundPost(t, pool, recipient, 250000, "topup", "order:paid:peer-unrelated")
	if err = s.ConfirmRefundTotal(ctx, "epay", o.TradeNo, "converted-peer-refund", "cny", o.AmountMinor); err != nil {
		t.Fatal(err)
	}
	assertConvertedRefund(t, pool, o, sub, 10000000, 0)
	var balance credits.Micro
	if err = pool.QueryRow(ctx, `SELECT balance FROM v3_billing.accounts WHERE id=$1`, recipient).Scan(&balance); err != nil || balance != 250000 {
		t.Fatalf("peer unrelated funds touched balance=%d err=%v", balance, err)
	}
}

func TestConvertedProviderRefundWaitsForHistoricalOwnerRefund(t *testing.T) {
	s, refunds, pool, _ := refundServices(t)
	ctx := context.Background()
	o, sub := convertedRefundPackage(t, s, pool)
	topup := paidRefundOrder(t, s, 1000, 0)
	account := refundAccount(t, pool)
	poster := ledger.NewPoster(pool)
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO v3_commerce.user_refunds(refund_no,order_id,user_id,account_id,provider_order_id,
		 gross_minor,fee_minor,amount_minor,refund_credits,reserved_credits,status)
		 VALUES('historical-owner', $1,1,$2,'remote-original',40,0,40,400000,400000,'processing')`, topup.ID, account)
		if err != nil {
			return err
		}
		_, err = poster.PostTx(ctx, tx, billing.Entry{AccountID: account, Amount: -400000, Kind: "refund", OperationID: "user-refund:historical-owner:reserve", Metadata: map[string]any{"refund_no": "historical-owner"}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ConfirmRefundTotal(ctx, "epay", o.TradeNo, "converted-wait-owner", "cny", o.AmountMinor); !errors.Is(err, commerce.ErrFundingPending) {
		t.Fatalf("pending refund revoked %v", err)
	}
	var balance, receipts int64
	var state string
	if err = pool.QueryRow(ctx, `SELECT balance,(SELECT count(*) FROM v3_billing.subscription_conversion_revocations),
	 (SELECT state FROM v3_commerce.orders WHERE id=$2) FROM v3_billing.accounts WHERE id=$1`, account, o.ID).Scan(&balance, &receipts, &state); err != nil || balance != 10600000 || receipts != 0 || state != "paid" {
		t.Fatalf("pending refund rollback wallet=%d receipts=%d state=%s err=%v", balance, receipts, state, err)
	}
	if _, err = poster.Post(ctx, billing.Entry{AccountID: account, Amount: 400000, Kind: "refund", OperationID: "user-refund:historical-owner:release", Metadata: map[string]any{"refund_no": "historical-owner", "refund_trade_no": topup.TradeNo}}); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE v3_commerce.user_refunds SET status='failed' WHERE refund_no='historical-owner'`); err != nil {
		t.Fatal(err)
	}
	if err = s.ConfirmRefundTotal(ctx, "epay", o.TradeNo, "converted-wait-owner", "cny", o.AmountMinor); err != nil {
		t.Fatal(err)
	}
	assertConvertedRefund(t, pool, o, sub, 10000000, 0)
	items, err := refunds.Eligible(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.TradeNo == topup.TradeNo && (item.RemainingQuota != 10000000 || !item.Refundable) {
			t.Fatalf("failed historical reserve polluted topup %+v", item)
		}
	}
}
