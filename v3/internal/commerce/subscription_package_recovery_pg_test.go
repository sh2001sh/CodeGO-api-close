//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

type packageCountingProvider struct {
	fakePayment
	calls atomic.Int64
	fail  atomic.Bool
}

func (p *packageCountingProvider) Checkout(ctx context.Context, o commerce.Order, success, cancel string) (commerce.Checkout, error) {
	p.calls.Add(1)
	if p.fail.Load() {
		return commerce.Checkout{}, commerce.ErrProviderUnavailable
	}
	return p.fakePayment.Checkout(ctx, o, success, cancel)
}

func TestSubscriptionPackageTerminalCheckoutsRestoreOriginalAllowance(t *testing.T) {
	for _, terminal := range []string{"canceled", "expired", "provider_failed"} {
		t.Run(terminal, func(t *testing.T) {
			pool := isolatedPool(t)
			ctx := context.Background()
			now := time.Date(2026, 9, 30, 4, 0, 0, 0, time.UTC)
			provider := &packageCountingProvider{}
			s := commerce.New(pool, ledger.NewPoster(pool), []commerce.PaymentProvider{provider}, commerce.Config{Now: func() time.Time { return now }, ReturnOrigins: []string{"https://site.test"}})
			p := packagePlan(t, s, 1000, 1000)
			packageCallback(t, s, create(t, s, p.ID))
			before := onlySubscription(t, s)
			spendPackage(t, pool, before.AccountID, 400, "terminal:spending")
			provider.fail.Store(terminal == "provider_failed")
			o, err := s.Create(ctx, packageRequest(p.ID, before.ID, "renew", "terminal-restore"))
			if terminal == "provider_failed" {
				if !errors.Is(err, commerce.ErrProviderUnavailable) {
					t.Fatalf("provider error lost: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if terminal == "canceled" {
					err = s.Cancel(ctx, 1, o.TradeNo)
				} else {
					now = now.Add(31 * time.Minute)
					_, err = s.ExpireOrders(ctx)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err = s.RecoverPackageCheckouts(ctx, 100); err != nil {
				t.Fatal(err)
			}
			after := onlySubscription(t, s)
			if after.ID != before.ID || after.AccountID == before.AccountID || after.Balance != 600 || after.UsedCredits != 400 || !after.ExpiresAt.Equal(before.ExpiresAt) {
				t.Fatalf("terminal checkout changed existing entitlement: %+v", after)
			}
			if sources, err := s.ActiveFundingSources(ctx); err != nil || len(sources) != 1 || sources[0].AccountID != after.AccountID {
				t.Fatalf("restored allowance unavailable: %+v err=%v", sources, err)
			}
			if n, err := s.RecoverPackageCheckouts(ctx, 100); err != nil || n != 0 || onlySubscription(t, s).AccountID != after.AccountID {
				t.Fatalf("restoration replay rotated again: n=%d err=%v", n, err)
			}
		})
	}
}

func TestSubscriptionPackageLatePaymentCommitsReceiptAndRequiresVerifiedRefund(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	p := packagePlan(t, s, 1000, 1000)
	packageCallback(t, s, create(t, s, p.ID))
	before := onlySubscription(t, s)
	spendPackage(t, pool, before.AccountID, 400, "late:spending")
	o, err := s.Create(ctx, packageRequest(p.ID, before.ID, "renew", "late-payment-review"))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Cancel(ctx, 1, o.TradeNo); err != nil {
		t.Fatal(err)
	}
	restored := onlySubscription(t, s)
	spendPackage(t, pool, restored.AccountID, 100, "late:resumed-spending")
	packageCallback(t, s, o)
	packageCallback(t, s, o)
	paid, err := s.GetOrder(ctx, 1, o.TradeNo)
	if err != nil || paid.State != "paid" || paid.FulfillmentState != "requires_review" {
		t.Fatalf("late signed receipt rejected or falsely fulfilled: %+v err=%v", paid, err)
	}
	after := onlySubscription(t, s)
	if after.AccountID != restored.AccountID || after.Balance != 500 {
		t.Fatalf("stale discounted checkout granted credit: %+v", after)
	}
	reviews, err := s.ListPackagePaymentReviews(ctx)
	if err != nil || len(reviews) != 1 || reviews[0].OrderID != o.ID || reviews[0].AmountMinor != 400 {
		t.Fatalf("durable review=%+v err=%v", reviews, err)
	}
	var receipts int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.payment_events WHERE trade_no=$1`, o.TradeNo).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatalf("late receipt replay=%d err=%v", receipts, err)
	}
	if err = s.ResolvePackagePaymentReview(ctx, o.ID); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("unrefunded review resolved: %v", err)
	}
	if err = s.ConfirmRefundTotal(ctx, "test", o.TradeNo, "review-verified-refund", o.Currency, o.AmountMinor); err != nil {
		t.Fatal(err)
	}
	if err = s.ResolvePackagePaymentReview(ctx, o.ID); err != nil {
		t.Fatal(err)
	}
	if reviews, err = s.ListPackagePaymentReviews(ctx); err != nil || len(reviews) != 0 || onlySubscription(t, s).Balance != 500 {
		t.Fatalf("verified refund damaged original allowance: reviews=%+v err=%v", reviews, err)
	}
}

func TestSubscriptionPackageConcurrentRequestKeyCreatesOneProviderCheckout(t *testing.T) {
	pool := isolatedPool(t)
	ctx := context.Background()
	provider := &packageCountingProvider{}
	s := commerce.New(pool, ledger.NewPoster(pool), []commerce.PaymentProvider{provider}, commerce.Config{ReturnOrigins: []string{"https://site.test"}})
	p := packagePlan(t, s, 1000, 1000)
	packageCallback(t, s, create(t, s, p.ID))
	before := onlySubscription(t, s)
	spendPackage(t, pool, before.AccountID, 400, "concurrent:spending")
	in := packageRequest(p.ID, before.ID, "renew", "same-package-request")
	provider.calls.Store(0)
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Create(ctx, in)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil && !errors.Is(err, commerce.ErrFundingPending) {
			t.Fatal(err)
		}
	}
	o, err := s.Create(ctx, in)
	if err != nil || o.PaymentURL == "" || provider.calls.Load() != 1 {
		t.Fatalf("provider duplicate checkout: calls=%d order=%+v err=%v", provider.calls.Load(), o, err)
	}
	for _, mutate := range []func(*commerce.CreateOrder){func(v *commerce.CreateOrder) { v.PlanID++ }, func(v *commerce.CreateOrder) { v.TargetSubscriptionID++ }, func(v *commerce.CreateOrder) { v.SuccessURL = "https://site.test/different" }} {
		other := in
		mutate(&other)
		if _, err = s.Create(ctx, other); !errors.Is(err, commerce.ErrStateConflict) {
			t.Fatalf("changed intent reused request key: %+v err=%v", other, err)
		}
	}
}
