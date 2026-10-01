//go:build pgintegration

package marketplace

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
)

func standardPoolFixture(t *testing.T, f *fixture, policy StandardPolicy) Pool {
	t.Helper()
	p, err := f.s.SavePool(testContext, Pool{Name: "legacy standard", Enabled: true, Scope: "standard", Price: 100, DailyLimit: 100,
		Rewards: []Reward{{Kind: "credits", Title: "ordinary", Weight: 1, Amount: 1000000, LegacyRewardType: "quota", WalletType: "default"}}, Standard: policy})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func standardOrdinaryDraw(limit int64) (int64, error) {
	if limit == zeroHourDrawLimit || limit == 1000000000 {
		return limit - 1, nil
	}
	return 0, nil
}

func TestStandardPolicyFreeStockDoesNotConsumeFirstAndOpenReplay(t *testing.T) {
	f := newFixture(t)
	p := standardPoolFixture(t, f, StandardPolicy{Enabled: true, FirstPurchaseMinimumMicro: 10000000, LowRewardThresholdMicro: 5000000, PityAfter: 5, PityMinimumMicro: 7000000})
	f.s.cfg.Draw = standardOrdinaryDraw
	if _, err := f.pool.Exec(testContext, `UPDATE v3_identity.users SET role='admin' WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.GrantBoxes(testContext, 2, 1, "standard-free", p.ID, 1); err != nil {
		t.Fatal(err)
	}
	free, err := f.s.OpenBoxes(testContext, 1, "free-open", 1)
	if err != nil || len(free) != 1 || free[0].Guarantee != "none" || free[0].Reward.Amount != 1000000 {
		t.Fatalf("free draw: %+v %v", free, err)
	}
	if _, err := f.s.PurchaseBoxes(testContext, 1, "paid", p.ID, 2); err != nil {
		t.Fatal(err)
	}
	paid, err := f.s.OpenBoxes(testContext, 1, "paid-open", 2)
	if err != nil || len(paid) != 2 {
		t.Fatalf("paid draw: %+v %v", paid, err)
	}
	if paid[0].Guarantee != "first" || paid[0].Reward.Amount != 10000000 || paid[0].Reward.RewardTier != "first_purchase" || paid[1].Reward.Amount != 1000000 || paid[1].Guarantee != "none" {
		t.Fatalf("first floor must apply once after free stock: %+v", paid)
	}
	before := f.balance(t, 1)
	replayed, err := f.s.OpenBoxes(testContext, 1, "paid-open", 2)
	if err != nil || len(replayed) != 2 || replayed[0].ID != paid[0].ID || f.balance(t, 1) != before {
		t.Fatalf("open replay: %+v %v", replayed, err)
	}
	if _, err := f.s.OpenBoxes(testContext, 1, "paid-open", 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed open replay: %v", err)
	}
	assertZeroState(t, f, 1, 10, 0, 0)
	view, err := f.s.Overview(testContext, 1)
	if err != nil || view.PityStates[p.ID].SmallProgress != 1 || view.PityStates[p.ID].Opened != 3 {
		t.Fatalf("low reward state: %+v %v", view.PityStates, err)
	}
}

func standardCashPurchase(t *testing.T, f *fixture, p Pool, trade string, count int) Purchase {
	t.Helper()
	order := boxOrderFixture(t, f, p.ID, trade, count)
	var purchase Purchase
	if err := pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		var err error
		purchase, err = f.s.CompleteBoxOrderTx(testContext, tx, 1, trade, order.AmountMinor, "usd")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return purchase
}

func TestStandardPolicyCashFirstGuaranteePreservesHighTier(t *testing.T) {
	f := newFixture(t)
	p := standardPoolFixture(t, f, StandardPolicy{Enabled: true, FirstPurchaseMinimumMicro: 10000000, LowRewardThresholdMicro: 5000000})
	p.Rewards[0].Amount = 15000000
	if _, err := f.s.SavePool(testContext, p); err != nil {
		t.Fatal(err)
	}
	standardCashPurchase(t, f, p, "first-cash", 2)
	f.s.cfg.Draw = standardOrdinaryDraw
	got, err := f.s.OpenBoxes(testContext, 1, "cash-open", 2)
	if err != nil || len(got) != 2 || got[0].Reward.Amount != 15000000 || got[0].Guarantee != "first" || got[1].Guarantee != "none" {
		t.Fatalf("first floor wrongly replaces larger tier: %+v %v", got, err)
	}
	if n := f.count(t, `SELECT opened_count FROM v3_marketplace.blind_box_orders WHERE trade_no='first-cash'`); n != 2 {
		t.Fatalf("cash counter %d", n)
	}
	assertZeroState(t, f, 1, 10, 0, 0)
}

func TestStandardPolicySubscriptionOverridesFirstAndPityAtExactBoundary(t *testing.T) {
	f := newFixture(t)
	if _, err := f.pool.Exec(testContext, `INSERT INTO v3_commerce.plans(id,name,price_minor,credits,period_seconds) VALUES(1,'retained monthly',100,1000000,2592000)`); err != nil {
		t.Fatal(err)
	}
	p := standardPoolFixture(t, f, StandardPolicy{Enabled: true, FirstPurchaseMinimumMicro: 10000000, PityAfter: 2, PityMinimumMicro: 7000000, LowRewardThresholdMicro: 5000000, SubscriptionPlanID: 1, SubscriptionProbabilityPPB: 1})
	purchase := standardCashPurchase(t, f, p, "precedence", 1)
	for _, tc := range []struct {
		n                   int64
		wantKind, guarantee string
		low                 int
	}{{0, "subscription", "none", 0}, {1, "credits", "small", 0}} {
		state := PityState{Opened: 4, SmallProgress: 1, BigProgress: 3}
		f.s.cfg.Draw = func(limit int64) (int64, error) {
			if limit != 1000000000 {
				return 0, fmt.Errorf("first/due must skip hidden draw, got %d", limit)
			}
			return tc.n, nil
		}
		var r Reward
		var guarantee string
		err := pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
			var err error
			r, guarantee, err = f.s.standardDrawTx(testContext, tx, 1, purchase.ID, p, &state)
			return err
		})
		if err != nil || r.Kind != tc.wantKind || guarantee != tc.guarantee || state.Opened != 5 || state.SmallProgress != tc.low || state.BigProgress != 0 {
			t.Fatalf("subscription threshold %d: %+v guarantee=%s state=%+v err=%v", tc.n, r, guarantee, state, err)
		}
		if r.Kind == "credits" && (r.Amount != 7000000 || r.RewardTier != "pity") {
			t.Fatalf("pity is exact configured amount: %+v", r)
		}
		if r.Kind == "subscription" && (r.PlanID != 1 || r.Title != "retained monthly") {
			t.Fatalf("subscription plan: %+v", r)
		}
	}
	assertZeroState(t, f, 1, 10, 0, 0)
}

func TestStandardPolicyFailedFulfillmentRollsBackProgressAndInventory(t *testing.T) {
	f := newFixture(t)
	p := standardPoolFixture(t, f, StandardPolicy{Enabled: true, FirstPurchaseMinimumMicro: 10000000, LowRewardThresholdMicro: 5000000})
	standardCashPurchase(t, f, p, "failure-cash", 1)
	f.s.cfg.Draw = standardOrdinaryDraw
	f.s.money = failRewards{base: f.poster}
	if _, err := f.s.OpenBoxes(testContext, 1, "failed-open", 1); err == nil {
		t.Fatal("injected ledger failure was accepted")
	}
	if n := f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_zero_hour_states`); n != 0 {
		t.Fatalf("failed progress persisted %d states", n)
	}
	if n := f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_open_records`); n != 0 {
		t.Fatalf("failed opening persisted %d records", n)
	}
	if n := f.count(t, `SELECT opened_count FROM v3_marketplace.blind_box_orders WHERE trade_no='failure-cash'`); n != 0 {
		t.Fatalf("failed order counter %d", n)
	}
	f.s.money = f.poster
	got, err := f.s.OpenBoxes(testContext, 1, "failed-open", 1)
	if err != nil || len(got) != 1 || got[0].Guarantee != "first" || got[0].Reward.Amount != 10000000 {
		t.Fatalf("successful retry: %+v %v", got, err)
	}
	assertZeroState(t, f, 1, 5, 0, 0)
}
