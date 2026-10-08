//go:build pgintegration

package commerce_test

import (
	"context"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestUserRefundImportedOriginSurvivesUnrelatedConversionRevocation(t *testing.T) {
	s, refunds, pool, _ := refundServices(t)
	ctx := context.Background()
	imported, err := s.Create(ctx, commerce.CreateOrder{UserID: 1, AmountMinor: 1000, Provider: "epay", SuccessURL: "https://site.test/ok", CancelURL: "https://site.test/cancel"})
	if err != nil {
		t.Fatal(err)
	}
	var account int64
	if err = pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',1,'wallet') RETURNING id`).Scan(&account); err != nil {
		t.Fatal(err)
	}
	opening, err := ledger.NewPoster(pool).Post(ctx, billing.Entry{AccountID: account, Amount: 10000000, Kind: "opening", OperationID: "migration:wallet:1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE v3_commerce.orders SET state='paid',payment_event_id='imported-remote-order' WHERE id=$1`, imported.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO v3_commerce.user_refund_origins(order_id,account_id,original_credits,remaining_credits,ledger_cursor)
	 VALUES($1,$2,10000000,3000000,$3)`, imported.ID, account, opening.EntryID); err != nil {
		t.Fatal(err)
	}
	o, sub := convertedRefundPackage(t, s, pool)
	if err = s.ConfirmRefundTotal(ctx, "epay", o.TradeNo, "conversion-preserve-imported", "cny", o.AmountMinor); err != nil {
		t.Fatal(err)
	}
	assertConvertedRefund(t, pool, o, sub, 10000000, 0)
	items, err := refunds.Eligible(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range items {
		if item.TradeNo != imported.TradeNo {
			continue
		}
		found = true
		if item.RemainingQuota != 3000000 || item.RefundAmountMinor != 294 || !item.Refundable {
			t.Fatalf("targeted conversion revocation consumed imported principal %+v", item)
		}
	}
	if !found {
		t.Fatal("imported principal omitted")
	}
	if r, err := refunds.Create(ctx, 1, commerce.UserRefundRequest{OrderType: "balance", TradeNo: imported.TradeNo}); err != nil || r.Status != "success" || r.AmountMinor != 294 {
		t.Fatalf("audited imported refund %+v err=%v", r, err)
	}
}
