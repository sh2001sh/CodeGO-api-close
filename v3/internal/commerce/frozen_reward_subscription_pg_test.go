//go:build pgintegration

package commerce_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestFrozenRewardSubscriptionFulfillsOriginalPromiseAfterPlanDisabled(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	p, err := s.SavePlan(ctx, commerce.Plan{Name: "Frozen reward", PolicyVersion: commerce.PolicyStandardV2,
		PriceMinor: 1000, Currency: "usd", Credits: 1030000, DurationUnit: "day", DurationValue: 7, Enabled: true,
		ModelLimits: map[string]int64{"gpt-test": 42}})
	if err != nil {
		t.Fatal(err)
	}
	var snapshot json.RawMessage
	if err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var freezeErr error
		snapshot, freezeErr = s.FreezeRewardPlanTx(ctx, tx, p.ID)
		return freezeErr
	}); err != nil {
		t.Fatal(err)
	}
	p.Enabled, p.Credits, p.Name, p.DurationValue = false, 1, "Changed catalog", 1
	p.ModelLimits = map[string]int64{"gpt-test": 1}
	if _, err = s.SavePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return s.GrantFrozenRewardTx(ctx, tx, 1, snapshot, "frozen-reward")
		}); err != nil {
			t.Fatal(err)
		}
	}
	subs, err := s.ListSubscriptions(ctx, 1)
	if err != nil || len(subs) != 1 {
		t.Fatalf("subscriptions: %+v %v", subs, err)
	}
	sub := subs[0]
	if sub.Balance != 1030000 || sub.TotalCredits != 1030000 || sub.NextResetAt != nil || sub.PolicyVersion != commerce.PolicyStandardV2 || sub.ExpiresAt.Sub(*now) != 7*24*time.Hour || sub.PlanSnapshot.Name != "Frozen reward" || sub.PlanSnapshot.ModelLimits["gpt-test"] != 42 {
		t.Fatalf("frozen promise changed: %+v", sub)
	}
	var granted, revenue, cards, receipts, limit int64
	if err = pool.QueryRow(ctx, `SELECT
	 (SELECT count(*) FROM v3_billing.ledger_entries WHERE operation_id='subscription:grant:frozen-reward'),
	 (SELECT recognized_revenue_credits FROM v3_commerce.subscriptions WHERE id=$1),
	 (SELECT count(*) FROM v3_marketplace.blind_box_props WHERE user_id=1 AND prop_type='monthly_pass_multiplier'),
	 (SELECT count(*) FROM v3_commerce.subscription_reward_receipts WHERE operation_id='frozen-reward'),
	 (SELECT (model_limits->>'gpt-test')::bigint FROM v3_commerce.subscriptions WHERE id=$1)`, sub.ID).Scan(&granted, &revenue, &cards, &receipts, &limit); err != nil {
		t.Fatal(err)
	}
	if granted != 1 || revenue != 0 || cards != 0 || receipts != 1 || limit != 42 {
		t.Fatalf("grant/revenue/cards/receipts/model limit: %d/%d/%d/%d/%d", granted, revenue, cards, receipts, limit)
	}
	var altered commerce.Plan
	if err = json.Unmarshal(snapshot, &altered); err != nil {
		t.Fatal(err)
	}
	altered.Credits++
	changed, err := json.Marshal(altered)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		user int64
		body json.RawMessage
	}{
		{"changed snapshot", 1, changed},
		{"another user", 2, snapshot},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
				return s.GrantFrozenRewardTx(ctx, tx, tc.user, tc.body, "frozen-reward")
			})
			if !errors.Is(err, billing.ErrPostConflict) {
				t.Fatalf("mismatched retry: %v", err)
			}
		})
	}
	if err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		_, freezeErr := s.FreezeRewardPlanTx(ctx, tx, p.ID)
		return freezeErr
	}); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("disabled plan admitted to new batch: %v", err)
	}
}

func TestFrozenRewardSubscriptionRejectsLegacyAndRollsBackGrant(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	legacy, err := s.SavePlan(ctx, commerce.Plan{Name: "Legacy", PriceMinor: 1000, Currency: "usd", Credits: 1000, PeriodSeconds: 86400, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		_, freezeErr := s.FreezeRewardPlanTx(ctx, tx, legacy.ID)
		return freezeErr
	}); !errors.Is(err, commerce.ErrInvalid) {
		t.Fatalf("legacy plan admitted to new batch: %v", err)
	}
	p, err := s.SavePlan(ctx, commerce.Plan{Name: "Fixed", PolicyVersion: commerce.PolicyStandardV2,
		PriceMinor: 1000, Currency: "usd", Credits: 1000, DurationUnit: "day", DurationValue: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	var snapshot json.RawMessage
	if err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var freezeErr error
		snapshot, freezeErr = s.FreezeRewardPlanTx(ctx, tx, p.ID)
		return freezeErr
	}); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("after grant rollback")
	if err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if grantErr := s.GrantFrozenRewardTx(ctx, tx, 1, snapshot, "rollback-frozen-reward"); grantErr != nil {
			return grantErr
		}
		return sentinel
	}); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	var subscriptions, entries, receipts int64
	if err = pool.QueryRow(ctx, `SELECT
	 (SELECT count(*) FROM v3_commerce.subscriptions WHERE reward_operation='rollback-frozen-reward'),
	 (SELECT count(*) FROM v3_billing.ledger_entries WHERE operation_id='subscription:grant:rollback-frozen-reward'),
	 (SELECT count(*) FROM v3_commerce.subscription_reward_receipts WHERE operation_id='rollback-frozen-reward')`).Scan(&subscriptions, &entries, &receipts); err != nil {
		t.Fatal(err)
	}
	if subscriptions != 0 || entries != 0 || receipts != 0 {
		t.Fatalf("partial fulfillment committed: %d/%d/%d", subscriptions, entries, receipts)
	}
}

func TestFrozenRewardSubscriptionConcurrentClaimDoesNotMergeLegacyPackage(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	legacy, err := s.SavePlan(ctx, commerce.Plan{Name: "Existing legacy", PriceMinor: 1000, Currency: "usd", Credits: 1000, PeriodSeconds: 86400, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return s.GrantRewardTx(ctx, tx, 1, legacy.ID, "existing-legacy-package")
	}); err != nil {
		t.Fatal(err)
	}
	p, err := s.SavePlan(ctx, commerce.Plan{Name: "Separate fixed reward", PolicyVersion: commerce.PolicyStandardV2,
		PriceMinor: 1000, Currency: "usd", Credits: 2000, DurationUnit: "day", DurationValue: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	var snapshot json.RawMessage
	if err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var freezeErr error
		snapshot, freezeErr = s.FreezeRewardPlanTx(ctx, tx, p.ID)
		return freezeErr
	}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	failures := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			failures <- pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
				return s.GrantFrozenRewardTx(ctx, tx, 1, snapshot, "concurrent-frozen-reward")
			})
		}()
	}
	wg.Wait()
	close(failures)
	for failure := range failures {
		if failure != nil {
			t.Fatal(failure)
		}
	}
	subs, err := s.ListSubscriptions(ctx, 1)
	if err != nil || len(subs) != 2 || subs[0].Balance != 2000 || subs[1].Balance != 1000 || subs[1].PolicyVersion != commerce.PolicyLegacy {
		t.Fatalf("concurrent grant duplicated or merged old package: %+v %v", subs, err)
	}
	var receipts int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.subscription_reward_receipts WHERE operation_id='concurrent-frozen-reward'`).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatalf("concurrent receipts: %d %v", receipts, err)
	}
}
