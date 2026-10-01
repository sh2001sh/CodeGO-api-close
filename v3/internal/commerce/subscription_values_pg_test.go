//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func monthlyPlan(t *testing.T, s *commerce.Service) commerce.Plan {
	t.Helper()
	p, err := s.SavePlan(context.Background(), commerce.Plan{Name: "monthly", PriceMinor: 1000, Currency: "usd", Credits: 1000, DurationUnit: "month", DurationValue: 1, Enabled: true, PlanType: "monthly"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSubscriptionConversionConcurrentReplayOwnershipAndSelectedPercent(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	p := monthlyPlan(t, s)
	o := create(t, s, p.ID)
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	sub := onlySubscription(t, s)
	if _, err := s.ConvertSubscription(ctx, 2, sub.ID, 20, "stolen"); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("foreign conversion: %v", err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := s.ConvertSubscription(ctx, 1, sub.ID, 20, "convert-20")
			if err == nil && (c.SourceCredits != 200 || c.TargetCredits != 2000000) {
				err = errors.New("wrong selected percent")
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	sub = onlySubscription(t, s)
	if sub.Balance != 800 || sub.UsedCredits != 200 || sub.State != "active" {
		t.Fatalf("partial conversion: %+v", sub)
	}
	if _, err := s.ConvertSubscription(ctx, 1, sub.ID, 21, "convert-20"); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("reused key different percent: %v", err)
	}
	if err := s.ResetSubscription(ctx, sub.ID, 1, "after-convert"); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("conversion quota refreshed: %v", err)
	}
	c, err := s.ConvertSubscription(ctx, 1, sub.ID, 80, "convert-all")
	if err != nil || c.SourceCredits != 800 || c.TargetCredits != 8000000 {
		t.Fatalf("remaining conversion %+v %v", c, err)
	}
	sub = onlySubscription(t, s)
	if sub.State != "canceled" || sub.Balance != 0 || sub.UsedCredits != 1000 {
		t.Fatalf("ended conversion %+v", sub)
	}
	var wallet int64
	if err = pool.QueryRow(ctx, `SELECT balance FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=1 AND kind='wallet'`).Scan(&wallet); err != nil || wallet != 10000000 {
		t.Fatalf("conversion wallet=%d err=%v", wallet, err)
	}
	history, err := s.ListSubscriptionConversions(ctx, 1, 50)
	if err != nil || len(history) != 2 {
		t.Fatalf("conversion history=%+v err=%v", history, err)
	}
}

func TestSubscriptionConversionRejectedPercentRestoresAndResetHistoryBlocks(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	p := monthlyPlan(t, s)
	o := create(t, s, p.ID)
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	sub := onlySubscription(t, s)
	old := sub.AccountID
	if _, err := ledger.NewPoster(pool).Post(ctx, billing.Entry{AccountID: old, Amount: -600, Kind: "usage", OperationID: "conversion:usage"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConvertSubscription(ctx, 1, sub.ID, 41, "too-high"); !errors.Is(err, commerce.ErrInvalid) {
		t.Fatalf("invalid remaining percent: %v", err)
	}
	sub = onlySubscription(t, s)
	if sub.AccountID == old || sub.Balance != 400 || sub.UsedCredits != 600 {
		t.Fatalf("rejection did not restore funds %+v", sub)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_commerce.subscriptions SET reset_opportunity_used=true WHERE id=$1`, sub.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConvertSubscription(ctx, 1, sub.ID, 40, "source-reset"); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("historical reset ignored: %v", err)
	}
	if n, err := s.RecoverSubscriptionChanges(ctx, 100); err != nil || n != 0 {
		t.Fatalf("terminal request endlessly retried: %d %v", n, err)
	}
}

type conversionDrain struct{ ready bool }

func (d *conversionDrain) FreezeAndDrained(context.Context, pgx.Tx, int64) (bool, error) {
	return d.ready, nil
}

func TestSubscriptionConversionDurableIntentRecoversAfterOutstandingUsage(t *testing.T) {
	_, pool, now := newService(t)
	ctx := context.Background()
	drain := &conversionDrain{}
	s := commerce.New(pool, ledger.NewPoster(pool), []commerce.PaymentProvider{fakePayment{}}, commerce.Config{Now: func() time.Time { return *now }, FundingDrain: drain, ReturnOrigins: []string{"https://site.test"}})
	p := monthlyPlan(t, s)
	o := create(t, s, p.ID)
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	sub := onlySubscription(t, s)
	if _, err := s.ConvertSubscription(ctx, 1, sub.ID, 20, "waiting-stream"); !errors.Is(err, commerce.ErrFundingPending) {
		t.Fatalf("outstanding settlement %v", err)
	}
	sources, err := s.ActiveFundingSources(ctx)
	if err != nil || len(sources) != 0 {
		t.Fatalf("pending conversion admitted funding %+v %v", sources, err)
	}
	if _, err := ledger.NewPoster(pool).Post(ctx, billing.Entry{AccountID: sub.AccountID, Amount: -100, Kind: "usage", OperationID: "conversion:held-usage"}); err != nil {
		t.Fatal(err)
	}
	drain.ready = true
	if n, err := s.RecoverSubscriptionChanges(ctx, 100); err != nil || n != 1 {
		t.Fatalf("durable recovery count=%d err=%v", n, err)
	}
	sub = onlySubscription(t, s)
	if sub.Balance != 700 || sub.UsedCredits != 300 {
		t.Fatalf("usage lost during conversion %+v", sub)
	}
	if n, err := s.RecoverSubscriptionChanges(ctx, 100); err != nil || n != 0 {
		t.Fatalf("replayed conversion count=%d err=%v", n, err)
	}
}

func TestSubscriptionFuelImmutableQuoteOnceAndRenewableReset(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	p := monthlyPlan(t, s)
	p.Credits = 1000 * credits.Micro(credits.PerCredit)
	p.FuelEnabled = true
	p.FuelUnitPriceMicro = 100000
	p.FuelMinCredits = 100 * credits.Micro(credits.PerCredit)
	p.FuelCreditStep = 50 * credits.Micro(credits.PerCredit)
	if _, err := s.SavePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	o := create(t, s, p.ID)
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	sub := onlySubscription(t, s)
	if _, err := pool.Exec(ctx, `UPDATE v3_commerce.subscriptions SET model_usage='{"chat":300}'::jsonb WHERE id=$1`, sub.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.QuoteSubscriptionFuel(ctx, 2, sub.ID, 250*credits.Micro(credits.PerCredit)); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("foreign fuel %v", err)
	}
	if _, err := s.QuoteSubscriptionFuel(ctx, 1, sub.ID, 249*credits.Micro(credits.PerCredit)); !errors.Is(err, commerce.ErrInvalid) {
		t.Fatalf("fuel step accepted %v", err)
	}
	fuel, err := s.CreateSubscriptionFuel(ctx, commerce.CreateOrder{UserID: 1, TargetSubscriptionID: sub.ID, FuelCredits: 250 * credits.Micro(credits.PerCredit), Provider: "test", SuccessURL: "https://site.test/success", CancelURL: "https://site.test/cancel"})
	if err != nil {
		t.Fatal(err)
	}
	if fuel.AmountMinor != 2500 || fuel.PurchaseType != "fuel" || fuel.TargetSubscriptionID != sub.ID {
		t.Fatalf("fuel quote %+v", fuel)
	}
	p.FuelUnitPriceMicro = 1000000
	if _, err = s.SavePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = s.Fulfill(ctx, "test", payment(fuel)); err != nil {
			t.Fatal(err)
		}
	}
	sub = onlySubscription(t, s)
	if sub.TotalCredits != 1250*credits.Micro(credits.PerCredit) || sub.Balance != sub.TotalCredits {
		t.Fatalf("fuel immutable grant %+v", sub)
	}
	var modelUsage int64
	if err = pool.QueryRow(ctx, `SELECT (model_usage->>'chat')::bigint FROM v3_commerce.subscriptions WHERE id=$1`, sub.ID).Scan(&modelUsage); err != nil || modelUsage != 300 {
		t.Fatalf("fuel reset existing model usage: %d %v", modelUsage, err)
	}
	if _, err = ledger.NewPoster(pool).Post(ctx, billing.Entry{AccountID: sub.AccountID, Amount: -1100 * credits.Micro(credits.PerCredit), Kind: "usage", OperationID: "fuel:spent"}); err != nil {
		t.Fatal(err)
	}
	if err = s.ResetSubscription(ctx, sub.ID, 1, "fuel-reset"); err != nil {
		t.Fatal(err)
	}
	sub = onlySubscription(t, s)
	if sub.UsedCredits != 100*credits.Micro(credits.PerCredit) || sub.Balance != 1150*credits.Micro(credits.PerCredit) {
		t.Fatalf("fuel was regenerated by reset %+v", sub)
	}
}

func TestSubscriptionFuelLatePaidChangedExpiryCommitsReview(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	p := monthlyPlan(t, s)
	p.Credits = 1000 * credits.Micro(credits.PerCredit)
	p.FuelEnabled = true
	p.FuelUnitPriceMicro = 100000
	p.FuelMinCredits = credits.Micro(credits.PerCredit)
	p.FuelCreditStep = credits.Micro(credits.PerCredit)
	if _, err := s.SavePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	o := create(t, s, p.ID)
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	sub := onlySubscription(t, s)
	fuel, err := s.CreateSubscriptionFuel(ctx, commerce.CreateOrder{UserID: 1, TargetSubscriptionID: sub.ID, FuelCredits: 10 * credits.Micro(credits.PerCredit), Provider: "test", SuccessURL: "https://site.test/success", CancelURL: "https://site.test/cancel"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE v3_commerce.subscriptions SET expires_at=$2 WHERE id=$1`, sub.ID, now.Add(40*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = s.Fulfill(ctx, "test", payment(fuel)); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetOrder(ctx, 1, fuel.TradeNo)
	if err != nil || got.State != "paid" || got.FulfillmentState != "requires_review" {
		t.Fatalf("fuel late payment %+v %v", got, err)
	}
	if onlySubscription(t, s).Balance != sub.Balance {
		t.Fatal("changed target received invalid fuel")
	}
}

func TestSubscriptionFuelVerifiedFullRefundRetainsBasePackage(t *testing.T) {
	s, _, _ := newService(t)
	ctx := context.Background()
	p := monthlyPlan(t, s)
	p.Credits = 1000 * credits.Micro(credits.PerCredit)
	p.FuelEnabled = true
	p.FuelUnitPriceMicro = 100000
	p.FuelMinCredits = credits.Micro(credits.PerCredit)
	p.FuelCreditStep = credits.Micro(credits.PerCredit)
	if _, err := s.SavePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	base := create(t, s, p.ID)
	if err := s.Fulfill(ctx, "test", payment(base)); err != nil {
		t.Fatal(err)
	}
	sub := onlySubscription(t, s)
	fuel, err := s.CreateSubscriptionFuel(ctx, commerce.CreateOrder{UserID: 1, TargetSubscriptionID: sub.ID, FuelCredits: 10 * credits.Micro(credits.PerCredit), Provider: "test", SuccessURL: "https://site.test/success", CancelURL: "https://site.test/cancel"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Fulfill(ctx, "test", payment(fuel)); err != nil {
		t.Fatal(err)
	}
	if err = s.ConfirmRefundTotal(ctx, "test", fuel.TradeNo, "fuel-partial", fuel.Currency, fuel.AmountMinor-1); !errors.Is(err, commerce.ErrPaymentMismatch) {
		t.Fatalf("partial fuel refund accepted: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err = s.ConfirmRefundTotal(ctx, "test", fuel.TradeNo, "fuel-full", fuel.Currency, fuel.AmountMinor); err != nil {
			t.Fatal(err)
		}
	}
	got := onlySubscription(t, s)
	if got.State != "active" || got.TotalCredits != p.Credits || got.Balance != p.Credits || got.AccountID == sub.AccountID {
		t.Fatalf("fuel refund lost base or duplicated reversal %+v", got)
	}
	original, err := s.GetOrder(ctx, 1, base.TradeNo)
	if err != nil || original.State != "paid" {
		t.Fatalf("base payment revoked %+v %v", original, err)
	}
}
