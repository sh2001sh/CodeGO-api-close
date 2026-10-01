//go:build pgintegration

package commerce_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func packagePlan(t *testing.T, s *commerce.Service, price int64, amount credits.Micro) commerce.Plan {
	t.Helper()
	p, err := s.SavePlan(context.Background(), commerce.Plan{Name: "monthly package", PriceMinor: price, Currency: "usd", Credits: amount, DurationUnit: "month", DurationValue: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func spendPackage(t *testing.T, pool *pgxpool.Pool, account int64, amount credits.Micro, operation string) {
	t.Helper()
	if _, err := ledger.NewPoster(pool).Post(context.Background(), billing.Entry{AccountID: account, Amount: -amount, Kind: "usage", OperationID: operation}); err != nil {
		t.Fatal(err)
	}
}

func packageRequest(plan, target int64, action, request string) commerce.CreateOrder {
	return commerce.CreateOrder{UserID: 1, PlanID: plan, Provider: "test", PurchaseAction: action, TargetSubscriptionID: target, RequestID: request, SuccessURL: "https://site.test/success", CancelURL: "https://site.test/cancel"}
}

func packageCallback(t *testing.T, s *commerce.Service, o commerce.Order) {
	t.Helper()
	body, err := json.Marshal(payment(o))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.HandleWebhook(context.Background(), "test", nil, body); err != nil {
		t.Fatal(err)
	}
}

func TestSubscriptionPackageRenewalRejectsBelowThresholdAndRestoresUnspentFunds(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	p := packagePlan(t, s, 1000, 1000)
	packageCallback(t, s, create(t, s, p.ID))
	before := onlySubscription(t, s)
	spendPackage(t, pool, before.AccountID, 299, "renew:under-threshold")
	o, err := s.Create(ctx, packageRequest(p.ID, before.ID, "renew", "threshold-rejection"))
	if !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("renewal at 29.9%% admitted: order=%+v err=%v", o, err)
	}
	after := onlySubscription(t, s)
	if after.ID != before.ID || after.AccountID == before.AccountID || after.Balance != 701 || after.UsedCredits != 299 || !after.ExpiresAt.Equal(before.ExpiresAt) {
		t.Fatalf("rejected quote did not preserve entitlement: before=%+v after=%+v", before, after)
	}
	var state, intent string
	if err = pool.QueryRow(ctx, `SELECT o.state,p.state FROM v3_commerce.orders o JOIN v3_commerce.package_checkouts p ON p.order_id=o.id WHERE o.id=$1`, o.ID).Scan(&state, &intent); err != nil || state != "failed" || intent != "restored" {
		t.Fatalf("rejected quote is not terminal: order=%s intent=%s err=%v", state, intent, err)
	}
	if n, err := s.RecoverPackageCheckouts(ctx, 100); err != nil || n != 0 {
		t.Fatalf("terminal rejected quote still needs recovery: n=%d err=%v", n, err)
	}
	spendPackage(t, pool, after.AccountID, 1, "renew:exact-threshold")
	accepted, err := s.Create(ctx, packageRequest(p.ID, after.ID, "renew", "threshold-exact"))
	if err != nil || accepted.AmountMinor != 300 {
		t.Fatalf("exact30%% renewal rejected: order=%+v err=%v", accepted, err)
	}
}

func TestSubscriptionPackageRenewalQuotesConsumedRatioAndWebhookGrantsOnce(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	p := packagePlan(t, s, 1000, 1000)
	packageCallback(t, s, create(t, s, p.ID))
	before := onlySubscription(t, s)
	spendPackage(t, pool, before.AccountID, 400, "renew:forty-percent")
	*now = now.Add(24 * time.Hour)
	in := packageRequest(p.ID, before.ID, "renew", "renewal-frozen-quote")
	o, err := s.Create(ctx, in)
	if err != nil || o.AmountMinor != 400 || o.PaymentURL == "" {
		t.Fatalf("40%% renewal quote=%+v err=%v", o, err)
	}
	sources, err := s.ActiveFundingSources(ctx)
	if err != nil || len(sources) != 0 {
		t.Fatalf("quoted old allowance remains admitted: %+v err=%v", sources, err)
	}
	if replay, err := s.Create(ctx, in); err != nil || replay.ID != o.ID || replay.PaymentURL != o.PaymentURL {
		t.Fatalf("checkout replay=%+v err=%v", replay, err)
	}
	bad := payment(o)
	bad.AmountMinor++
	body, err := json.Marshal(bad)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.HandleWebhook(ctx, "test", nil, body); !errors.Is(err, commerce.ErrPaymentMismatch) {
		t.Fatalf("quote mismatch accepted: %v", err)
	}
	packageCallback(t, s, o)
	after := onlySubscription(t, s)
	packageCallback(t, s, o)
	replayed := onlySubscription(t, s)
	if after.ID != before.ID || after.AccountID == before.AccountID || after.Balance != 1000 || after.UsedCredits != 0 || !after.StartsAt.Equal(*now) || !after.ExpiresAt.Equal(now.AddDate(0, 1, 0)) || replayed.AccountID != after.AccountID {
		t.Fatalf("renewed lifecycle/replay: before=%+v after=%+v replay=%+v", before, after, replayed)
	}
	var oldBalance, grants, receipts int64
	if err = pool.QueryRow(ctx, `SELECT balance,(SELECT count(*) FROM v3_billing.ledger_entries WHERE kind='subscription_grant'),(SELECT count(*) FROM v3_commerce.payment_events WHERE trade_no=$2) FROM v3_billing.accounts WHERE id=$1`, before.AccountID, o.TradeNo).Scan(&oldBalance, &grants, &receipts); err != nil || oldBalance != 0 || grants != 2 || receipts != 1 {
		t.Fatalf("duplicate/retired money: old=%d grants=%d receipts=%d err=%v", oldBalance, grants, receipts, err)
	}
}

func TestSubscriptionPackageUpgradePreservesImmutableDiscountCreditsAndCalendar(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	old := packagePlan(t, s, 1001, 1000)
	packageCallback(t, s, create(t, s, old.ID))
	before := onlySubscription(t, s)
	spendPackage(t, pool, before.AccountID, 333, "upgrade:one-third")
	newPlan := packagePlan(t, s, 2000, 2000)
	o, err := s.Create(ctx, packageRequest(newPlan.ID, before.ID, "upgrade", "upgrade-frozen-quote"))
	// 2000 - 1001 * 667 / 1000 = 1332.333, rounded once to1332.
	if err != nil || o.AmountMinor != 1332 || o.Credits != 2000 {
		t.Fatalf("exact upgrade discount=%+v err=%v", o, err)
	}
	newPlan.PriceMinor, newPlan.Credits, newPlan.DurationValue = 9999, 9999, 12
	if _, err = s.SavePlan(ctx, newPlan); err != nil {
		t.Fatal(err)
	}
	packageCallback(t, s, o)
	after := onlySubscription(t, s)
	if after.ID != before.ID || after.PlanID != newPlan.ID || after.Balance != 2000 || after.TotalCredits != 2000 || !after.ExpiresAt.Equal(now.AddDate(0, 1, 0)) {
		t.Fatalf("plan edits changed paid entitlement: %+v", after)
	}
}

func TestSubscriptionPackageManualResetChargesFullPriceAndUpgradePreservesRemaining(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	p := packagePlan(t, s, 1000, 1000)
	packageCallback(t, s, create(t, s, p.ID))
	sub := onlySubscription(t, s)
	spendPackage(t, pool, sub.AccountID, 400, "reset-pricing:spending")
	if err := s.ResetSubscription(ctx, sub.ID, 2, "reset-pricing:manual"); err != nil {
		t.Fatal(err)
	}
	renew, err := s.Create(ctx, packageRequest(p.ID, sub.ID, "renew", "reset-pricing:renew"))
	if err != nil || renew.AmountMinor != 1000 {
		t.Fatalf("manual reset received second discounted cycle: order=%+v err=%v", renew, err)
	}
	if err = s.Cancel(ctx, 1, renew.TradeNo); err != nil {
		t.Fatal(err)
	}
	upgraded := packagePlan(t, s, 2000, 2000)
	o, err := s.Create(ctx, packageRequest(upgraded.ID, sub.ID, "upgrade", "reset-pricing:upgrade"))
	if err != nil || o.AmountMinor != 2000 {
		t.Fatalf("manual reset upgrade received discount: order=%+v err=%v", o, err)
	}
	packageCallback(t, s, o)
	after := onlySubscription(t, s)
	if after.TotalCredits != 3000 || after.Balance != 3000 {
		t.Fatalf("full-price upgrade discarded1000 remaining reset credits: %+v", after)
	}
}
