//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func configureConversion(t *testing.T, s *commerce.Service, id int64, source, wallet, paid credits.Micro, refreshed *credits.Micro) commerce.WalletConversionQuote {
	t.Helper()
	ctx := context.Background()
	q, err := s.QuoteWalletConversion(ctx, 1, id)
	if err != nil {
		t.Fatal(err)
	}
	if q.State != "needs_review" || q.BasisKey == "" {
		t.Fatalf("unexpected unconfigured quote %+v", q)
	}
	subs, err := s.ListSubscriptions(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	plan := int64(0)
	for _, sub := range subs {
		if sub.ID == id {
			plan = sub.PlanID
		}
	}
	_, err = s.SaveRedesignRules(ctx, commerce.RedesignRules{ConversionRules: []commerce.ConversionRule{{PlanID: plan, BasisKey: q.BasisKey, SourceCredits: source, WalletCredits: wallet, PaidWalletCredits: paid, RefreshedPaidWalletCredits: refreshed, Enabled: true, Reviewed: true, Note: "audited test entitlement"}}})
	if err != nil {
		t.Fatal(err)
	}
	q, err = s.QuoteWalletConversion(ctx, 1, id)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func TestStandardV2IndependentFrozenGrantNeverResets(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	legacy := monthlyPlan(t, s)
	old := create(t, s, legacy.ID)
	if err := s.Fulfill(ctx, "test", payment(old)); err != nil {
		t.Fatal(err)
	}
	p, err := s.SavePlan(ctx, commerce.Plan{Name: "fixed", PolicyVersion: commerce.PolicyStandardV2, PriceMinor: 10000, Currency: "usd", Credits: 103000000, DurationUnit: "day", DurationValue: 90, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	o := create(t, s, p.ID)
	p.Credits = 110000000
	p.PriceMinor = 11000
	if _, err = s.SavePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err = s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	subs, err := s.ListSubscriptions(ctx, 1)
	if err != nil || len(subs) != 2 {
		t.Fatalf("independent packages=%+v %v", subs, err)
	}
	fresh := subs[0]
	if fresh.PolicyVersion != commerce.PolicyStandardV2 || fresh.TotalCredits != 103000000 || fresh.Balance != 103000000 || fresh.NextResetAt != nil {
		t.Fatalf("mutable grant %+v", fresh)
	}
	if fresh.PlanSnapshot.ID != p.ID || fresh.PlanSnapshot.Credits != 103000000 || fresh.PlanSnapshot.PriceMinor != 10000 {
		t.Fatalf("public subscription lost its original advertised specification: %+v", fresh.PlanSnapshot)
	}
	if o.RecognizedRevenueCredits == nil || *o.RecognizedRevenueCredits != 100000000 {
		t.Fatalf("revenue not frozen %+v", o)
	}
	if err = s.ResetSubscription(ctx, fresh.ID, 1, "cannot-reset-v2"); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("manual v2 reset %v", err)
	}
	if _, err = s.QuoteSubscriptionFuel(ctx, 1, fresh.ID, 100); !errors.Is(err, commerce.ErrInvalid) {
		t.Fatalf("v2 fuel %v", err)
	}
	var policy string
	if err = pool.QueryRow(ctx, `SELECT policy_version FROM v3_commerce.subscription_buckets WHERE account_id=$1`, fresh.AccountID).Scan(&policy); err != nil || policy != commerce.PolicyStandardV2 {
		t.Fatalf("bucket policy %q %v", policy, err)
	}
	p.PolicyVersion = commerce.PolicyLegacy
	if _, err = s.SavePlan(ctx, p); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("issued policy mutated %v", err)
	}
	if err = s.ResetSubscription(ctx, subs[1].ID, 1, "old-still-reset"); err != nil {
		t.Fatalf("old reset changed %v", err)
	}
}

func TestWholeWalletConversionConsentReplaySourcesAndNoReset(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	p := monthlyPlan(t, s)
	o := create(t, s, p.ID)
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	sub := onlySubscription(t, s)
	if _, err := ledger.NewPoster(pool).Post(ctx, billing.Entry{AccountID: sub.AccountID, Amount: -400, Kind: "usage", OperationID: "whole:spent"}); err != nil {
		t.Fatal(err)
	}
	q := configureConversion(t, s, sub.ID, 1000, 1000000, 800000, nil)
	if q.State != "quoted" || q.SourceCredits != 600 || q.TargetCredits != 600000 || q.PaidCredits != 480000 || q.RewardCredits != 120000 {
		t.Fatalf("quote %+v", q)
	}
	if _, err := s.ConfirmWalletConversion(ctx, 1, q.QuoteID, "whole-denied", false); !errors.Is(err, commerce.ErrInvalid) {
		t.Fatalf("consent bypass %v", err)
	}
	if _, err := s.ConfirmWalletConversion(ctx, 2, q.QuoteID, "whole-stolen", true); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("ownership bypass %v", err)
	}
	p.PriceMinor = 5000
	if _, err := s.SavePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, e := s.ConfirmWalletConversion(ctx, 1, q.QuoteID, "whole-once", true)
			if e == nil && (c.State != "completed" || c.TargetCredits != 600000) {
				e = errors.New("wrong conversion result")
			}
			errs <- e
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
	if sub.State != "canceled" || sub.ConvertedAt == nil || sub.Balance != 0 || sub.BenefitsUntil == nil {
		t.Fatalf("old package remains spendable %+v", sub)
	}
	if err := s.ResetSubscription(ctx, sub.ID, 1, "converted-reset"); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("converted reset %v", err)
	}
	var paid, reward, total int64
	if err := pool.QueryRow(ctx, `SELECT COALESCE(sum(remaining_amount) FILTER(WHERE NOT non_transferable),0)::bigint,COALESCE(sum(remaining_amount) FILTER(WHERE non_transferable AND non_refundable),0)::bigint,count(*) FROM v3_billing.funding_lots WHERE source='subscription_conversion'`).Scan(&paid, &reward, &total); err != nil || paid != 480000 || reward != 120000 || total != 2 {
		t.Fatalf("provenance paid=%d reward=%d lots=%d %v", paid, reward, total, err)
	}
	if _, err := s.ConfirmWalletConversion(ctx, 1, q.QuoteID, "whole-other", true); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("repeat rights %v", err)
	}
}

func TestWholeWalletConversionExpiryDuringDrainRejectsAndRecoveryTerminates(t *testing.T) {
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
	q := configureConversion(t, s, sub.ID, 1000, 1000000, 1000000, nil)
	c, err := s.ConfirmWalletConversion(ctx, 1, q.QuoteID, "whole-pending", true)
	if !errors.Is(err, commerce.ErrFundingPending) || c.State != "pending" {
		t.Fatalf("pending=%+v %v", c, err)
	}
	*now = sub.ExpiresAt
	drain.ready = true
	if n, err := s.RecoverSubscriptionChanges(ctx, 100); err != nil || n != 0 {
		t.Fatalf("expired recovery n=%d %v", n, err)
	}
	c, err = s.WalletConversion(ctx, 1, "whole-pending")
	if err != nil || c.State != "failed" {
		t.Fatalf("expired durable result %+v %v", c, err)
	}
	if _, err = s.QuoteWalletConversion(ctx, 1, sub.ID); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("exact expiry quote %v", err)
	}
	var total int64
	if err = pool.QueryRow(ctx, `SELECT COALESCE(sum(balance),0)::bigint FROM v3_billing.accounts WHERE kind='wallet' AND owner_type='user' AND owner_id=1`).Scan(&total); err != nil || total != 0 {
		t.Fatalf("expired credited %d %v", total, err)
	}
	if n, err := s.RecoverSubscriptionChanges(ctx, 100); err != nil || n != 0 {
		t.Fatalf("failed replay loops n=%d %v", n, err)
	}
}
