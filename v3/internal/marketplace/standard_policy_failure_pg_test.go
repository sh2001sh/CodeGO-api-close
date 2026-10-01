//go:build pgintegration

package marketplace

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestStandardPolicyHiddenRewardPrecedesSubscriptionAndMissingPlanRollsBack(t *testing.T) {
	f := newFixture(t)
	p := standardPoolFixture(t, f, StandardPolicy{Enabled: true, SubscriptionPlanID: 99, SubscriptionProbabilityPPB: 1000000000})
	purchase := standardCashPurchase(t, f, p, "hidden-priority", 1)
	state := PityState{Opened: 5, SmallProgress: 3}
	f.s.cfg.Draw = func(limit int64) (int64, error) {
		if limit != zeroHourDrawLimit {
			return 0, fmt.Errorf("hidden hit must not draw subscription, got %d", limit)
		}
		return 0, nil
	}
	err := pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		r, g, err := f.s.standardDrawTx(testContext, tx, 1, purchase.ID, p, &state)
		if err != nil {
			return err
		}
		if r.PropType != zeroHourPropType || r.RewardTier != "zero_hour_hidden" || g != "none" {
			return fmt.Errorf("hidden result %+v %s", r, g)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if state.Opened != 6 || state.SmallProgress != 0 || state.BigProgress != 0 {
		t.Fatalf("hidden pity state %+v", state)
	}
	assertZeroState(t, f, 1, 0, 0, 1)
	f.s.cfg.Draw = func(limit int64) (int64, error) {
		if limit == zeroHourDrawLimit {
			return limit - 1, nil
		}
		return 0, nil
	}
	before := state
	err = pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		_, _, err := f.s.standardDrawTx(testContext, tx, 1, purchase.ID, p, &state)
		return err
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing subscription plan: %v", err)
	}
	if state != before {
		t.Fatalf("failed plan changed pity from %+v to %+v", before, state)
	}
	assertZeroState(t, f, 1, 0, 0, 1)
	for _, draw := range []int64{-1, 1000000000} {
		f.s.cfg.Draw = func(limit int64) (int64, error) {
			if limit == zeroHourDrawLimit {
				return limit - 1, nil
			}
			return draw, nil
		}
		err := pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
			_, _, err := f.s.standardDrawTx(testContext, tx, 1, purchase.ID, p, &state)
			return err
		})
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid subscription draw %d: %v", draw, err)
		}
	}
	assertZeroState(t, f, 1, 0, 0, 1)
}

func TestStandardPolicyMigratedNegativePurchaseIDRemainsOpenable(t *testing.T) {
	f := newFixture(t)
	p := standardPoolFixture(t, f, StandardPolicy{Enabled: true, FirstPurchaseMinimumMicro: 10000000, LowRewardThresholdMicro: 5000000})
	if _, err := f.pool.Exec(testContext, `INSERT INTO v3_marketplace.blind_box_orders(id,user_id,pool_id,quantity,amount_minor,trade_no,status,source) VALUES(42,1,$1,1,250,'legacy-cash','success','purchase')`, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(testContext, `INSERT INTO v3_marketplace.blind_box_purchases(id,user_id,pool_id,quantity,unit_price_micro,purchase_date,request_id,external_order_id) VALUES(-42,1,$1,1,0,'2026-09-30','legacy-order:42',42)`, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(testContext, `INSERT INTO v3_marketplace.blind_box_items(purchase_id,owner_user_id,purchase_user_id,pool_id,rewards,guarantees) SELECT -42,1,1,id,rewards,guarantees FROM v3_marketplace.blind_box_pools WHERE id=$1`, p.ID); err != nil {
		t.Fatal(err)
	}
	f.s.cfg.Draw = standardOrdinaryDraw
	got, err := f.s.OpenBoxes(testContext, 1, "legacy-first-open", 1)
	if err != nil || len(got) != 1 || got[0].Guarantee != "first" || got[0].Reward.Amount != 10000000 {
		t.Fatalf("negative retained purchase: %+v %v", got, err)
	}
	if n := f.count(t, `SELECT opened_count FROM v3_marketplace.blind_box_orders WHERE id=42`); n != 1 {
		t.Fatalf("legacy order count: %d", n)
	}
}
