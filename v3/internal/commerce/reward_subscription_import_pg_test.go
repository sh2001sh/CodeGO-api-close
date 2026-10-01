//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestRewardSubscriptionImportedPendingRequiresFrozenBenefitSnapshot(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	s.SetMonthlyBenefits(rewardMarket(s, pool, now))
	p := rewardMonthlyPlan(t, s, pool, "pro", 1000, 1000, 0)
	o := create(t, s, p.ID)
	if _, err := pool.Exec(ctx, `DELETE FROM v3_commerce.monthly_purchase_benefits WHERE order_id=$1`, o.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Fulfill(ctx, "test", payment(o)); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("unsnapshotted imported checkout silently dropped benefits: %v", err)
	}
	if n, err := s.InitializeImportedMonthlyBenefits(ctx); err != nil || n != 1 {
		t.Fatalf("import initialization n=%d err=%v", n, err)
	}
	if n, err := s.InitializeImportedMonthlyBenefits(ctx); err != nil || n != 0 {
		t.Fatalf("import replay n=%d err=%v", n, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_commerce.plans SET membership_tier='ultra' WHERE id=$1`, p.ID); err != nil {
		t.Fatal(err)
	}
	packageCallback(t, s, o)
	var seconds int64
	if err := pool.QueryRow(ctx, `SELECT remaining_seconds FROM v3_marketplace.blind_box_props WHERE user_id=1 AND prop_type='monthly_pass_multiplier'`).Scan(&seconds); err != nil {
		t.Fatal(err)
	}
	if seconds != 2700 {
		t.Fatalf("imported checkout failed frozen benefit seconds=%d", seconds)
	}
	if n, err := s.InitializeImportedMonthlyBenefits(ctx); err != nil || n != 0 {
		t.Fatalf("paid history incorrectly reinitialized n=%d err=%v", n, err)
	}
}
