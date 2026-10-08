//go:build pgintegration

package main

import (
	"context"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/incentives"
)

func TestRestoreComposedRewardRoutesAndOpportunityOwnership(t *testing.T) {
	s := restoreStack(t, nil)
	alice, bob := s.register(t, "restored_rewards_alice"), s.register(t, "restored_rewards_bob")
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `INSERT INTO v3_commerce.subscription_reset_opportunity_accounts(user_id,available_total,earned_total) VALUES($1,2,2)`, alice.User.ID); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/user/aff/rewards", "/api/subscription/self/reset-opportunity"} {
		s.call(t, "GET", path, "", "", 401)
		s.call(t, "GET", path, alice.AccessToken, "", 200)
	}
	owner := restoreData[incentives.ResetSummary](t, s.call(t, "GET", "/api/subscription/self/reset-opportunity", alice.AccessToken, "", 200))
	other := restoreData[incentives.ResetSummary](t, s.call(t, "GET", "/api/subscription/self/reset-opportunity", bob.AccessToken, "", 200))
	if owner.AvailableCount != 2 || owner.EarnedTotal != 2 || other.AvailableCount != 0 {
		t.Fatalf("opportunity owner drift: owner=%+v other=%+v", owner, other)
	}
	s.call(t, "POST", "/api/subscription/self/reset-opportunity/use", bob.AccessToken, `{}`, 409)
	s.call(t, "POST", "/api/subscription/self/reset-opportunity/use", alice.AccessToken, `{}`, 404)
	for _, path := range []string{"/api/daily-lucky-number/self", "/api/daily-lucky-number/history", "/api/daily-lucky-number/admin/config"} {
		s.call(t, "GET", path, "", "", 404)
		s.call(t, "GET", path, alice.AccessToken, "", 404)
	}
	s.call(t, "PUT", "/api/daily-lucky-number/admin/config", alice.AccessToken, `{}`, 404)
	s.call(t, "POST", "/api/daily-lucky-number/admin/backfill", alice.AccessToken, `{}`, 404)
	owner = restoreData[incentives.ResetSummary](t, s.call(t, "GET", "/api/subscription/self/reset-opportunity", alice.AccessToken, "", 200))
	if owner.AvailableCount != 2 || owner.UsedTotal != 0 {
		t.Fatalf("failed reward route spent opportunity: %+v", owner)
	}
}
