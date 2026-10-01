//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func onlySubscription(t *testing.T, s *commerce.Service) commerce.Subscription {
	t.Helper()
	subs, err := s.ListSubscriptions(context.Background(), 1)
	if err != nil || len(subs) != 1 {
		t.Fatalf("subscriptions=%+v err=%v", subs, err)
	}
	return subs[0]
}

func TestSubscriptionCycleEnforcesLifetimeAndPeriodBudgetsAndFrozenRules(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	p, err := s.SavePlan(ctx, commerce.Plan{Name: "dual budget", PriceMinor: 100, Currency: "usd", Credits: 1000, PeriodCredits: 400, PeriodSeconds: 600, ResetPeriod: "custom", ResetCustomSeconds: 60, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	o := create(t, s, p.ID)
	p.Credits, p.PeriodCredits, p.ResetCustomSeconds = 10000, 800, 120
	if _, err = s.SavePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err = s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	sub := onlySubscription(t, s)
	if sub.Balance != 400 || sub.TotalCredits != 1000 || sub.ResetCustomSeconds != 60 || sub.NextResetAt == nil || !sub.NextResetAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("mutable or unbounded initial quota: %+v", sub)
	}
	poster := ledger.NewPoster(pool)
	spent := []credits.Micro{300, 400, 300}
	wanted := []credits.Micro{400, 300, 0}
	for i, amount := range spent {
		old := sub.AccountID
		if _, err = poster.Post(ctx, billing.Entry{AccountID: old, Amount: -amount, Kind: "usage", OperationID: fmt.Sprintf("cycle:usage:%d", i)}); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `UPDATE v3_commerce.subscriptions SET model_usage='{"chat":300}'::jsonb WHERE id=$1`, sub.ID); err != nil {
			t.Fatal(err)
		}
		*now = now.Add(time.Minute)
		if n, err := s.ResetDueSubscriptions(ctx, 100); err != nil || n != 1 {
			t.Fatalf("cycle %d count=%d err=%v", i, n, err)
		}
		sub = onlySubscription(t, s)
		if sub.Balance != wanted[i] || sub.AccountID == old {
			t.Fatalf("cycle %d allowance: %+v", i, sub)
		}
		var modelUsageEmpty bool
		if err = pool.QueryRow(ctx, `SELECT model_usage='{}'::jsonb FROM v3_commerce.subscriptions WHERE id=$1`, sub.ID).Scan(&modelUsageEmpty); err != nil || !modelUsageEmpty {
			t.Fatalf("cycle %d retained previous model allowance usage: %v %v", i, modelUsageEmpty, err)
		}
		var oldBalance int64
		if err = pool.QueryRow(ctx, `SELECT balance FROM v3_billing.accounts WHERE id=$1`, old).Scan(&oldBalance); err != nil || oldBalance != 0 {
			t.Fatalf("retired bucket balance=%d err=%v", oldBalance, err)
		}
		if n, err := s.ResetDueSubscriptions(ctx, 100); err != nil || n != 0 {
			t.Fatalf("cycle replay count=%d err=%v", n, err)
		}
	}
	if sub.UsedCredits != 1000 {
		t.Fatalf("lifetime consumption lost: %+v", sub)
	}
}

func TestSubscriptionLegacyCyclesSkipMissedGrantsAndStopAtExpiration(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	p, err := s.SavePlan(ctx, commerce.Plan{Name: "legacy cycle", PriceMinor: 100, Currency: "usd", Credits: 1000, PeriodSeconds: 600, ResetPeriod: "custom", ResetCustomSeconds: 60, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	o := create(t, s, p.ID)
	if err = s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	sub := onlySubscription(t, s)
	if _, err = ledger.NewPoster(pool).Post(ctx, billing.Entry{AccountID: sub.AccountID, Amount: -900, Kind: "usage", OperationID: "legacy-usage"}); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(5*time.Minute + 10*time.Second)
	if n, err := s.ResetDueSubscriptions(ctx, 100); err != nil || n != 1 {
		t.Fatalf("missed cycle count=%d err=%v", n, err)
	}
	sub = onlySubscription(t, s)
	if sub.Balance != 1000 || sub.UsedCredits != 0 || !sub.LegacyPeriodic || sub.NextResetAt == nil || !sub.NextResetAt.Equal(sub.StartsAt.Add(6*time.Minute)) {
		t.Fatalf("incorrect missed cycle: %+v", sub)
	}
	var grants int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_billing.ledger_entries WHERE kind='subscription_grant'`).Scan(&grants); err != nil || grants != 2 {
		t.Fatalf("missed cycles accrued extra funds: %d %v", grants, err)
	}
	*now = sub.ExpiresAt
	if n, err := s.ResetDueSubscriptions(ctx, 100); err != nil || n != 0 {
		t.Fatalf("reset expired subscription count=%d err=%v", n, err)
	}
	if n, err := s.ExpireSubscriptions(ctx, 100); err != nil || n != 1 {
		t.Fatalf("expiry count=%d err=%v", n, err)
	}
}

func TestSubscriptionAdminIdempotencyPreferencesAndOwnership(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	p, err := s.SavePlan(ctx, commerce.Plan{Name: "internal", PriceMinor: 100, Currency: "usd", Credits: 1000, PeriodSeconds: 600, Enabled: true, InternalOnly: true, MaxPurchasePerUser: 1})
	if err != nil {
		t.Fatal(err)
	}
	plans, err := s.ListPlans(ctx, false)
	if err != nil || len(plans) != 0 {
		t.Fatalf("internal plan leaked: %+v %v", plans, err)
	}
	if _, err = s.Create(ctx, commerce.CreateOrder{UserID: 1, PlanID: p.ID, Provider: "test", SuccessURL: "https://site.test/s", CancelURL: "https://site.test/c"}); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("internal checkout: %v", err)
	}
	id, err := s.BindSubscription(ctx, 1, p.ID, "grant-key")
	if err != nil {
		t.Fatal(err)
	}
	if replay, err := s.BindSubscription(ctx, 1, p.ID, "grant-key"); err != nil || replay != id {
		t.Fatalf("grant replay id=%d err=%v", replay, err)
	}
	if _, err = s.BindSubscription(ctx, 2, p.ID, "grant-key"); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("cross-user grant replay: %v", err)
	}
	if _, err = s.BindSubscription(ctx, 1, p.ID, "second-key"); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("grant purchase limit: %v", err)
	}
	sub := onlySubscription(t, s)
	if _, err = ledger.NewPoster(pool).Post(ctx, billing.Entry{AccountID: sub.AccountID, Amount: -500, Kind: "usage", OperationID: "admin-test-spending"}); err != nil {
		t.Fatal(err)
	}
	if err = s.ResetSubscription(ctx, id, 2, "reset-key"); err != nil {
		t.Fatal(err)
	}
	reset := onlySubscription(t, s)
	if reset.Balance != 1000 || reset.AccountID == sub.AccountID {
		t.Fatalf("admin reset did not rotate bucket: %+v", reset)
	}
	if err = s.ResetSubscription(ctx, id, 2, "reset-key"); err != nil {
		t.Fatal(err)
	}
	if after := onlySubscription(t, s); after.AccountID != reset.AccountID {
		t.Fatalf("admin replay credited again: %+v", after)
	}
	pref, err := s.SetSubscriptionPreference(ctx, 1, commerce.SubscriptionPreference{BillingPreference: "wallet_first", SubscriptionOrderIDs: []int64{id}})
	if err != nil || pref.BillingPreference != "wallet_first" {
		t.Fatalf("preference %+v err=%v", pref, err)
	}
	if _, err = s.SetSubscriptionPreference(ctx, 2, commerce.SubscriptionPreference{SubscriptionOrderIDs: []int64{id}}); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("foreign subscription preference: %v", err)
	}
	prefs, orders, err := s.FundingPreferences(ctx)
	if err != nil || prefs[1] != "wallet_first" || len(orders[1]) != 1 || orders[1][0] != reset.AccountID {
		t.Fatalf("funding profile prefs=%v orders=%v err=%v", prefs, orders, err)
	}
	if err = s.DeletePlan(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	plans, err = s.ListPlans(ctx, true)
	if err != nil || len(plans) != 1 || plans[0].Enabled {
		t.Fatalf("historical plan deleted: %+v %v", plans, err)
	}
	if err = s.DeleteSubscription(ctx, id); err != nil {
		t.Fatal(err)
	}
	subs, err := s.ListSubscriptions(ctx, 1)
	if err != nil || len(subs) != 0 {
		t.Fatalf("deleted subscription still visible: %+v %v", subs, err)
	}
}

func TestSubscriptionCheckoutLimitSerializesConcurrentOrders(t *testing.T) {
	s, _, _ := newService(t)
	ctx := context.Background()
	p, err := s.SavePlan(ctx, commerce.Plan{Name: "one purchase", PriceMinor: 100, Currency: "usd", Credits: 1000, PeriodSeconds: 600, Enabled: true, MaxPurchasePerUser: 1})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 20)
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Create(ctx, commerce.CreateOrder{UserID: 1, PlanID: p.ID, Provider: "test", SuccessURL: "https://site.test/s", CancelURL: "https://site.test/c"})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, commerce.ErrStateConflict) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("purchase limit admitted %d simultaneous checkouts", success)
	}
}
