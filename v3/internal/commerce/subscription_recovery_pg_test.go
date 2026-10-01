//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

type toggleFundingDrain struct{ drained bool }

func (d *toggleFundingDrain) FreezeAndDrained(context.Context, pgx.Tx, int64) (bool, error) {
	return d.drained, nil
}

func TestSubscriptionManualMutationRecoversAfterCallerLeaves(t *testing.T) {
	pool := isolatedPool(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 30, 4, 0, 0, 0, time.UTC)
	drain := &toggleFundingDrain{}
	s := commerce.New(pool, ledger.NewPoster(pool), nil, commerce.Config{Now: func() time.Time { return now }, FundingDrain: drain})
	p, err := s.SavePlan(ctx, commerce.Plan{Name: "manual recovery", PriceMinor: 100, Currency: "usd", Credits: 1000, PeriodSeconds: 600, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.BindSubscription(ctx, 1, p.ID, "manual-recovery-grant")
	if err != nil {
		t.Fatal(err)
	}
	sub := onlySubscription(t, s)
	if _, err = ledger.NewPoster(pool).Post(ctx, billing.Entry{AccountID: sub.AccountID, Amount: -500, Kind: "usage", OperationID: "manual-recovery-spent"}); err != nil {
		t.Fatal(err)
	}
	if err = s.ResetSubscription(ctx, id, 2, "manual-recovery-reset"); !errors.Is(err, commerce.ErrFundingPending) {
		t.Fatalf("pending reset err=%v", err)
	}
	if n, err := s.RecoverSubscriptionChanges(ctx, 100); err != nil || n != 0 {
		t.Fatalf("still pending count=%d err=%v", n, err)
	}
	drain.drained = true
	if n, err := s.RecoverSubscriptionChanges(ctx, 100); err != nil || n != 1 {
		t.Fatalf("recovered count=%d err=%v", n, err)
	}
	reset := onlySubscription(t, s)
	if reset.Balance != 1000 || reset.AccountID == sub.AccountID {
		t.Fatalf("reset recovery %+v", reset)
	}
	if n, err := s.RecoverSubscriptionChanges(ctx, 100); err != nil || n != 0 {
		t.Fatalf("recovery replay count=%d err=%v", n, err)
	}
	edit := commerce.EditSubscription{StartsAt: now, ExpiresAt: now.Add(15 * time.Minute), State: "active", TotalCredits: 2000, UsedCredits: 400, PeriodCredits: 600, PeriodUsed: 100, RequestID: "edit-recovery"}
	drain.drained = false
	if err = s.UpdateSubscription(ctx, id, 2, edit); !errors.Is(err, commerce.ErrFundingPending) {
		t.Fatalf("pending edit err=%v", err)
	}
	drain.drained = true
	if n, err := s.RecoverSubscriptionChanges(ctx, 100); err != nil || n != 1 {
		t.Fatalf("edit recovered count=%d err=%v", n, err)
	}
	after := onlySubscription(t, s)
	if after.Balance != 500 || after.TotalCredits != 2000 || after.UsedCredits != 400 || after.PeriodUsed != 100 {
		t.Fatalf("edit budgets %+v", after)
	}
	edit.TotalCredits = 3000
	if err = s.UpdateSubscription(ctx, id, 2, edit); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("changed replay err=%v", err)
	}
}

func TestSubscriptionUnlimitedLifetimeKeepsFiniteCycleAllowance(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	p, err := s.SavePlan(ctx, commerce.Plan{Name: "finite cycle", PriceMinor: 100, Currency: "usd", Credits: 0, PeriodCredits: 400, PeriodSeconds: 600, ResetPeriod: "custom", ResetCustomSeconds: 60, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	o := create(t, s, p.ID)
	if err = s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	sub := onlySubscription(t, s)
	if sub.Balance != 400 || sub.TotalCredits != 0 {
		t.Fatalf("unbounded grant %+v", sub)
	}
	if _, err = ledger.NewPoster(pool).Post(ctx, billing.Entry{AccountID: sub.AccountID, Amount: -400, Kind: "usage", OperationID: "unlimited-cycle-usage"}); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Minute)
	if n, err := s.ResetDueSubscriptions(ctx, 100); err != nil || n != 1 {
		t.Fatalf("unlimited cycle count=%d err=%v", n, err)
	}
	sub = onlySubscription(t, s)
	if sub.Balance != 400 || sub.UsedCredits != 400 {
		t.Fatalf("unlimited cycle %+v", sub)
	}
}

func TestSubscriptionCancelDeleteDurablyRecoverAndReactivatedBucketCanEndAgain(t *testing.T) {
	pool := isolatedPool(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 30, 4, 0, 0, 0, time.UTC)
	drain := &toggleFundingDrain{}
	s := commerce.New(pool, ledger.NewPoster(pool), nil, commerce.Config{Now: func() time.Time { return now }, FundingDrain: drain})
	p, err := s.SavePlan(ctx, commerce.Plan{Name: "end recovery", PriceMinor: 100, Currency: "usd", Credits: 1000, PeriodSeconds: 600, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.BindSubscription(ctx, 1, p.ID, "end-grant")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.EndSubscription(ctx, id, 2, false); !errors.Is(err, commerce.ErrFundingPending) {
		t.Fatalf("pending cancellation %v", err)
	}
	if sources, err := s.ActiveFundingSources(ctx); err != nil || len(sources) != 0 {
		t.Fatalf("pending cancellation funded %+v %v", sources, err)
	}
	if _, err = s.ConvertSubscription(ctx, 1, id, 20, "evade-cancel"); !errors.Is(err, commerce.ErrFundingPending) {
		t.Fatalf("conversion bypassed pending cancel %v", err)
	}
	drain.drained = true
	if n, err := s.RecoverSubscriptionChanges(ctx, 100); err != nil || n != 1 {
		t.Fatalf("cancellation recovery %d %v", n, err)
	}
	sub := onlySubscription(t, s)
	if sub.State != "canceled" || sub.Balance != 0 {
		t.Fatalf("unrecovered cancellation %+v", sub)
	}
	edit := commerce.EditSubscription{StartsAt: now, ExpiresAt: now.Add(time.Hour), State: "active", TotalCredits: 2000, RequestID: "reactivate"}
	if err = s.UpdateSubscription(ctx, id, 2, edit); err != nil {
		t.Fatal(err)
	}
	drain.drained = false
	if err = s.EndSubscription(ctx, id, 2, true); !errors.Is(err, commerce.ErrFundingPending) {
		t.Fatalf("pending delete %v", err)
	}
	drain.drained = true
	if n, err := s.RecoverSubscriptionChanges(ctx, 100); err != nil || n != 1 {
		t.Fatalf("delete recovery %d %v", n, err)
	}
	if subs, err := s.ListSubscriptions(ctx, 1); err != nil || len(subs) != 0 {
		t.Fatalf("deleted subscription visible %+v %v", subs, err)
	}
	if n, err := s.RecoverSubscriptionChanges(ctx, 100); err != nil || n != 0 {
		t.Fatalf("end replay %d %v", n, err)
	}
}

func TestSubscriptionConflictingTerminalIntentsSerializeBeforeFreeze(t *testing.T) {
	_, pool, now := newService(t)
	ctx := context.Background()
	drain := &toggleFundingDrain{}
	s := commerce.New(pool, ledger.NewPoster(pool), []commerce.PaymentProvider{fakePayment{}}, commerce.Config{Now: func() time.Time { return *now }, FundingDrain: drain, ReturnOrigins: []string{"https://site.test"}})
	p := monthlyPlan(t, s)
	o := create(t, s, p.ID)
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	sub := onlySubscription(t, s)
	start := make(chan struct{})
	errs := make(chan error, 16)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			var err error
			if i%2 == 0 {
				err = s.EndSubscription(ctx, sub.ID, 2, false)
			} else {
				_, err = s.ConvertSubscription(ctx, 1, sub.ID, 20, fmt.Sprintf("conflict-conversion-%d", i))
			}
			errs <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if !errors.Is(err, commerce.ErrFundingPending) {
			t.Fatalf("conflicting intent %v", err)
		}
	}
	var pending int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.subscription_operations WHERE subscription_id=$1 AND state='pending'`, sub.ID).Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("conflicting durable intents=%d err=%v", pending, err)
	}
	drain.drained = true
	if n, err := s.RecoverSubscriptionChanges(ctx, 100); err != nil || n != 1 {
		t.Fatalf("serialized recovery %d %v", n, err)
	}
	if n, err := s.RecoverSubscriptionChanges(ctx, 100); err != nil || n != 0 {
		t.Fatalf("duplicate recovery %d %v", n, err)
	}
}
