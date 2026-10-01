//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/internal/marketplace"
)

func TestGroupEligibilityChecksOwnershipAndFreezesPaidOrderRules(t *testing.T) {
	s, pool, now := newService(t)
	s.SetGroupCheckoutMarket(marketplace.New(pool, ledger.NewPoster(pool), nil, s, s, marketplace.Config{Now: func() time.Time { return *now }}))
	ctx := context.Background()
	p, err := s.SavePlan(ctx, commerce.Plan{Name: "group plan", PriceMinor: 200, Currency: "usd", Credits: 2_000_000, PeriodSeconds: 3600, Enabled: true,
		GroupBuyEnabled: true, GroupBuyTarget: 2, GroupBuyBonus: 500_000, GroupBuyLifetimeSeconds: 1800})
	if err != nil {
		t.Fatal(err)
	}
	o := create(t, s, p.ID)
	if err = s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	p.GroupBuyEnabled = false
	p.GroupBuyBonus = 9_000_000
	if _, err = s.SavePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := s.GroupPurchaseTx(ctx, tx, 2, o.ID); !errors.Is(err, marketplace.ErrNotFound) {
			t.Fatalf("foreign order eligibility err=%v", err)
		}
		result, err := s.GroupPurchaseTx(ctx, tx, 1, o.ID)
		if err != nil {
			return err
		}
		if !result.Enabled || result.BonusMicro != 500_000 || result.TargetCount != 2 || result.SubscriptionID <= 0 {
			t.Fatalf("rules changed after payment: %+v", result)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMonthCardRewardAndOperationReplayShareLedgerTransaction(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	p, err := s.SavePlan(ctx, commerce.Plan{Name: "reward plan", PriceMinor: 100, Currency: "usd", Credits: 1_000_000, PeriodSeconds: 3600, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return s.GrantRewardTx(ctx, tx, 1, p.ID, "prop:1") }); err != nil {
			t.Fatal(err)
		}
	}
	var subscriptions, entries int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM v3_commerce.subscriptions),(SELECT count(*) FROM v3_billing.ledger_entries)`).Scan(&subscriptions, &entries); err != nil {
		t.Fatal(err)
	}
	if subscriptions != 1 || entries != 1 {
		t.Fatalf("subscriptions=%d entries=%d", subscriptions, entries)
	}
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return s.GrantRewardTx(ctx, tx, 2, p.ID, "prop:1") })
	if !errors.Is(err, billing.ErrPostConflict) {
		t.Fatalf("reward operation moved to another user: %v", err)
	}
}
