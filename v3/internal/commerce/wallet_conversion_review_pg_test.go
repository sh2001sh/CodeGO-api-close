//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestReviewedWalletConversionFuelSourcesAndRefund(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	p := monthlyPlan(t, s)
	p.FuelEnabled, p.FuelUnitPriceMicro, p.FuelMinCredits, p.FuelCreditStep = true, 10000000000, 100, 100
	if _, err := s.SavePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	o := create(t, s, p.ID)
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	sub := onlySubscription(t, s)
	fuel, err := s.CreateSubscriptionFuel(ctx, commerce.CreateOrder{UserID: 1, TargetSubscriptionID: sub.ID, FuelCredits: 100, Provider: "test", SuccessURL: "https://site.test/success", CancelURL: "https://site.test/cancel"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Fulfill(ctx, "test", payment(fuel)); err != nil {
		t.Fatal(err)
	}
	evidence, err := s.WalletConversionReviewEvidence(ctx, sub.ID)
	if err != nil || evidence.CurrentCredits != 1100 || evidence.FutureCredits != 0 || len(evidence.Sources) != 2 {
		t.Fatalf("evidence %+v err=%v", evidence, err)
	}
	review := commerce.WalletConversionReview{SubscriptionID: sub.ID, FactHash: evidence.FactHash, Note: "original paid and supplemental orders checked", Enabled: true, Reviewed: true, Segments: []commerce.WalletConversionSegment{
		{Name: "original", OriginalOrderID: o.ID, SourceTotal: 1000, CurrentCredits: 1000, WalletCredits: 1000, PaidWalletCredits: 800},
		{Name: "fuel", OriginalOrderID: fuel.ID, SourceTotal: 100, CurrentCredits: 100, WalletCredits: 100, PaidWalletCredits: 100},
	}}
	review, err = s.SaveWalletConversionReview(ctx, 1, review)
	if err != nil || review.ID == 0 || review.ReviewerID != 1 {
		t.Fatalf("review %+v err=%v", review, err)
	}
	q, err := s.QuoteWalletConversion(ctx, 1, sub.ID)
	if err != nil || q.State != "quoted" || q.ReviewID != review.ID || q.TargetCredits != 1100 || q.PaidCredits != 900 || len(q.Segments) != 2 {
		t.Fatalf("reviewed quote %+v err=%v", q, err)
	}
	if _, err = s.ConfirmWalletConversion(ctx, 2, q.QuoteID, "review-foreign", true); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("ownership bypass %v", err)
	}
	for range 2 {
		c, err := s.ConfirmWalletConversion(ctx, 1, q.QuoteID, "review-complete", true)
		if err != nil || c.State != "completed" || c.TargetCredits != 1100 {
			t.Fatalf("completion %+v err=%v", c, err)
		}
	}
	var originals, supplemental int64
	if err = pool.QueryRow(ctx, `SELECT COALESCE(sum(remaining_amount) FILTER(WHERE metadata->>'original_order_id'=$1),0)::bigint,COALESCE(sum(remaining_amount) FILTER(WHERE metadata->>'original_order_id'=$2),0)::bigint FROM v3_billing.funding_lots WHERE source='subscription_conversion'`, strconv.FormatInt(o.ID, 10), strconv.FormatInt(fuel.ID, 10)).Scan(&originals, &supplemental); err != nil || originals != 1000 || supplemental != 100 {
		t.Fatalf("separate origins original=%d supplemental=%d err=%v", originals, supplemental, err)
	}
	if err = s.ConfirmRefundTotal(ctx, "test", fuel.TradeNo, "review-fuel-refund", fuel.Currency, fuel.AmountMinor); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT balance FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=1 AND kind='wallet'`).Scan(&originals); err != nil || originals != 1000 {
		t.Fatalf("fuel refund changed original source wallet=%d err=%v", originals, err)
	}
}

func TestReviewedWalletConversionFuturePromisesAndZeroCurrent(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	p, err := s.SavePlan(ctx, commerce.Plan{Name: "periodic", PriceMinor: 1000, Currency: "usd", Credits: 1000, PeriodCredits: 300, ResetPeriod: "daily", DurationUnit: "day", DurationValue: 4, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	o := create(t, s, p.ID)
	if err = s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	sub := onlySubscription(t, s)
	if _, err = ledger.NewPoster(pool).Post(ctx, billing.Entry{AccountID: sub.AccountID, Amount: -300, Kind: "usage", OperationID: "review:spent-current"}); err != nil {
		t.Fatal(err)
	}
	evidence, err := s.WalletConversionReviewEvidence(ctx, sub.ID)
	if err != nil || evidence.CurrentCredits != 0 || evidence.FutureCredits != 700 {
		t.Fatalf("future evidence %+v err=%v", evidence, err)
	}
	review := commerce.WalletConversionReview{SubscriptionID: sub.ID, FactHash: evidence.FactHash, Note: "four-day lifetime cap and future periods checked", Enabled: true, Reviewed: true, Segments: []commerce.WalletConversionSegment{{Name: "future", OriginalOrderID: o.ID, SourceTotal: 1000, FutureCredits: 700, WalletCredits: 1000, PaidWalletCredits: 1000}}}
	missing := review
	missing.Segments = []commerce.WalletConversionSegment{{Name: "missing", SourceTotal: 1000, FutureCredits: 600, WalletCredits: 1000}}
	if _, err = s.SaveWalletConversionReview(ctx, 1, missing); !errors.Is(err, commerce.ErrInvalid) {
		t.Fatalf("unissued promises omitted %v", err)
	}
	if _, err = s.SaveWalletConversionReview(ctx, 1, review); err != nil {
		t.Fatal(err)
	}
	q, err := s.QuoteWalletConversion(ctx, 1, sub.ID)
	if err != nil || q.State != "quoted" || q.SourceCredits != 0 || q.FutureCredits != 700 || q.TargetCredits != 700 {
		t.Fatalf("future quote %+v err=%v", q, err)
	}
	c, err := s.ConfirmWalletConversion(ctx, 1, q.QuoteID, "review-promises", true)
	if err != nil || c.State != "completed" || c.TargetCredits != 700 {
		t.Fatalf("future conversion %+v err=%v", c, err)
	}
	*now = now.Add(24 * time.Hour)
	if n, err := s.ResetDueSubscriptions(ctx, 100); err != nil || n != 0 {
		t.Fatalf("converted promise granted again n=%d err=%v", n, err)
	}
	if err = s.ResetSubscription(ctx, sub.ID, 1, "review-after-converted"); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("reviewed subscription refreshed %v", err)
	}
}

func TestReviewedWalletConversionFactsAndExpiryRemainAuthoritative(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	p := monthlyPlan(t, s)
	o := create(t, s, p.ID)
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	sub := onlySubscription(t, s)
	evidence, err := s.WalletConversionReviewEvidence(ctx, sub.ID)
	if err != nil {
		t.Fatal(err)
	}
	review := commerce.WalletConversionReview{SubscriptionID: sub.ID, FactHash: evidence.FactHash, Enabled: true, Reviewed: true, Note: "audited", Segments: []commerce.WalletConversionSegment{{Name: "original", SourceTotal: 1000, CurrentCredits: 1000, WalletCredits: 1000}}}
	if _, err = s.SaveWalletConversionReview(ctx, 1, review); err != nil {
		t.Fatal(err)
	}
	if _, err = ledger.NewPoster(pool).Post(ctx, billing.Entry{AccountID: sub.AccountID, Amount: -1, Kind: "usage", OperationID: "review:late-usage"}); err != nil {
		t.Fatal(err)
	}
	if q, err := s.QuoteWalletConversion(ctx, 1, sub.ID); err != nil || q.State != "needs_review" {
		t.Fatalf("stale review quoted %+v err=%v", q, err)
	}
	if _, err = s.SaveWalletConversionReview(ctx, 1, review); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("stale facts approved %v", err)
	}
	*now = sub.ExpiresAt
	if _, err = s.WalletConversionReviewEvidence(ctx, sub.ID); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("expired administrative review %v", err)
	}
	if _, err = s.SaveWalletConversionReview(ctx, 1, review); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("expired administrative approval %v", err)
	}
}

func TestReviewedWalletConversionAcceptedPendingRetainsVersionEvidence(t *testing.T) {
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
	evidence, err := s.WalletConversionReviewEvidence(ctx, sub.ID)
	if err != nil {
		t.Fatal(err)
	}
	review, err := s.SaveWalletConversionReview(ctx, 1, commerce.WalletConversionReview{SubscriptionID: sub.ID, FactHash: evidence.FactHash, Enabled: true, Reviewed: true, Note: "first reviewed entitlement", Segments: []commerce.WalletConversionSegment{{Name: "original", SourceTotal: 1000, CurrentCredits: 1000, WalletCredits: 1000}}})
	if err != nil {
		t.Fatal(err)
	}
	q, err := s.QuoteWalletConversion(ctx, 1, sub.ID)
	if err != nil || q.State != "quoted" {
		t.Fatalf("quote %+v err=%v", q, err)
	}
	if _, err = s.ConfirmWalletConversion(ctx, 1, q.QuoteID, "review-frozen-pending", true); !errors.Is(err, commerce.ErrFundingPending) {
		t.Fatal(err)
	}
	review.Enabled, review.Note = false, "stop new quotes"
	if _, err = s.SaveWalletConversionReview(ctx, 1, review); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ConfirmWalletConversion(ctx, 1, q.QuoteID, "review-not-accepted", true); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("stopped review accepted a new conversion %v", err)
	}
	drain.ready = true
	if n, err := s.RecoverSubscriptionChanges(ctx, 100); err != nil || n != 1 {
		t.Fatalf("frozen accepted review recovery n=%d err=%v", n, err)
	}
	c, err := s.WalletConversion(ctx, 1, "review-frozen-pending")
	if err != nil || c.State != "completed" || c.TargetCredits != 1000 {
		t.Fatalf("frozen reviewed conversion %+v err=%v", c, err)
	}
	var note string
	var count int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM v3_commerce.subscription_wallet_review_versions WHERE review_id=$1),review_snapshot->>'note' FROM v3_commerce.subscription_wallet_review_versions WHERE review_id=$1 AND revision=1`, review.ID).Scan(&count, &note); err != nil || count != 2 || note != "first reviewed entitlement" {
		t.Fatalf("review history overwritten count=%d note=%q err=%v", count, note, err)
	}
}
