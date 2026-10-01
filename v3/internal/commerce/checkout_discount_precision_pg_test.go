//go:build pgintegration

package commerce_test

import (
	"context"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestCheckoutDiscountFullRatePreservesSource99PercentClamp(t *testing.T) {
	s, pool, _ := newService(t)
	checkoutCard(t, pool, "topup_discount", 1000000)
	o := create(t, s, 0)
	if o.AmountMinor != 12 || o.Credits != 12000000 {
		t.Fatalf("100%% source clamp=%+v", o)
	}
	d, err := s.GetCheckoutDiscount(context.Background(), 1, o.TradeNo)
	if err != nil || d.Multiplier != "0.010000" || !d.OriginalKnown || d.OriginalMinor != 1200 {
		t.Fatalf("clamp snapshot=%+v err=%v", d, err)
	}
}

func TestCheckoutDiscountStablecoinMinimumUsesExactCurrencyPrecision(t *testing.T) {
	_, pool, now := newService(t)
	checkoutCard(t, pool, "topup_discount", 999999)
	s := commerce.New(pool, ledger.NewPoster(pool), []commerce.PaymentProvider{fakePayment{}}, commerce.Config{Now: func() time.Time { return *now }, Currency: "usdt", TopupCreditsPerMinor: 1, ReturnOrigins: []string{"https://site.test"}})
	o, err := s.Create(context.Background(), commerce.CreateOrder{UserID: 1, AmountMinor: 1000000, Provider: "test", SuccessURL: "https://site.test/s", CancelURL: "https://site.test/c"})
	if err != nil || o.AmountMinor != 10000 || o.Credits != 1000000 || o.Currency != "usdt" {
		t.Fatalf("stablecoin .01 minimum=%+v %v", o, err)
	}
	if err = s.Fulfill(context.Background(), "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	var balance int64
	if err = pool.QueryRow(context.Background(), `SELECT balance FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=1 AND kind='wallet'`).Scan(&balance); err != nil || balance != 1000000 {
		t.Fatalf("currency discountchangedgrant %d %v", balance, err)
	}
}
