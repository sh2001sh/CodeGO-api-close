//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/marketplace"
)

// Model a checkout created before retirement, whose benefit was already frozen.
func retainMonthlySnapshot(t *testing.T, pool *pgxpool.Pool, order, target, source int64) {
	t.Helper()
	tag, err := pool.Exec(context.Background(), `UPDATE v3_commerce.monthly_purchase_benefits SET target_seconds=$2,source_seconds=$3 WHERE order_id=$1`, order, target, source)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("retained checkout snapshot: %v %v", tag, err)
	}
}

func TestMonthlyCardRetirementNewPurchaseAndRewardGrantNoCard(t *testing.T) {
	for _, tier := range []string{"lite", "standard", "pro", "ultra"} {
		t.Run(tier, func(t *testing.T) {
			s, pool, now := newService(t)
			ctx := context.Background()
			s.SetMonthlyBenefits(failedMonthlyBenefit{rewardMarket(s, pool, now), errors.New("retired card grant must not run")})
			p := rewardMonthlyPlan(t, s, pool, tier, 1000, 1000, 0)
			o := create(t, s, p.ID)
			packageCallback(t, s, o)
			packageCallback(t, s, o)
			for range 2 {
				if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return s.GrantRewardTx(ctx, tx, 1, p.ID, "retired-card-reward") }); err != nil {
					t.Fatal(err)
				}
			}
			var cards, benefitSeconds, rewardSeconds, balance int64
			if err := pool.QueryRow(ctx, `SELECT
			 (SELECT count(*) FROM v3_marketplace.blind_box_props WHERE prop_type='monthly_pass_multiplier'),
			 (SELECT target_seconds FROM v3_commerce.monthly_purchase_benefits WHERE order_id=$1),
			 (SELECT monthly_seconds FROM v3_commerce.subscription_reward_receipts WHERE operation_id='retired-card-reward'),
			 (SELECT balance FROM v3_billing.accounts WHERE id=$2)`, o.ID, onlySubscription(t, s).AccountID).Scan(&cards, &benefitSeconds, &rewardSeconds, &balance); err != nil {
				t.Fatal(err)
			}
			if cards != 0 || benefitSeconds != 0 || rewardSeconds != 0 || balance != 2000 {
				t.Fatalf("new grants cards=%d frozen=%d reward=%d balance=%d", cards, benefitSeconds, rewardSeconds, balance)
			}
		})
	}
}

func TestMonthlyCardRetirementNewRenewalAndUpgradePreserveExistingCard(t *testing.T) {
	for _, action := range []string{"renew", "upgrade"} {
		t.Run(action, func(t *testing.T) {
			s, pool, now := newService(t)
			ctx := context.Background()
			market := rewardMarket(s, pool, now)
			s.SetMonthlyBenefits(market)
			if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return market.GrantMonthlyCardTx(ctx, tx, 1, 120, "retained-issued-card") }); err != nil {
				t.Fatal(err)
			}
			if _, err := market.UseProp(ctx, 2, 1); !errors.Is(err, marketplace.ErrNotFound) {
				t.Fatalf("foreign card activation: %v", err)
			}
			active, err := market.UseProp(ctx, 1, 1)
			if err != nil || active.Status != "active" || active.MultiplierPPM != 100000 {
				t.Fatalf("retained activation: %+v %v", active, err)
			}
			current := rewardMonthlyPlan(t, s, pool, "standard", 1000, 1000, 0)
			packageCallback(t, s, create(t, s, current.ID))
			before := onlySubscription(t, s)
			spendPackage(t, pool, before.AccountID, 400, "retired-card-used")
			target := current
			if action == "upgrade" {
				target = rewardMonthlyPlan(t, s, pool, "pro", 2000, 2000, 0)
			}
			o, err := s.Create(ctx, packageRequest(target.ID, before.ID, action, "retired-card-"+action))
			if err != nil {
				t.Fatal(err)
			}
			packageCallback(t, s, o)
			packageCallback(t, s, o)
			var seconds, receipts int64
			var expiry time.Time
			if err := pool.QueryRow(ctx, `SELECT remaining_seconds,expires_at,
			 (SELECT count(*) FROM v3_marketplace.operations WHERE kind='monthly_card')
			 FROM v3_marketplace.blind_box_props WHERE id=1`).Scan(&seconds, &expiry, &receipts); err != nil {
				t.Fatal(err)
			}
			if seconds != 120 || receipts != 1 || !expiry.Equal(*active.ExpiresAt) {
				t.Fatalf("existing card changed: seconds=%d receipts=%d expiry=%v", seconds, receipts, expiry)
			}
			*now = now.Add(10 * time.Second)
			paused, err := market.PauseProp(ctx, 1, 1)
			if err != nil || paused.Status != "paused" || paused.RemainingSeconds != 110 {
				t.Fatalf("retained pause: %+v %v", paused, err)
			}
			resumed, err := market.UseProp(ctx, 1, 1)
			if err != nil || !resumed.ExpiresAt.Equal(expiry) || resumed.MultiplierPPM != 100000 {
				t.Fatalf("retained resume: %+v %v", resumed, err)
			}
			*now = expiry
			if _, err := market.UseProp(ctx, 1, 1); !errors.Is(err, marketplace.ErrConflict) {
				t.Fatalf("expired card revived: %v", err)
			}
		})
	}
}
