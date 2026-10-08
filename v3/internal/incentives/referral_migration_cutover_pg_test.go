//go:build pgintegration

package incentives

import (
	"context"
	"testing"
	"time"
)

func TestMigrationCutoverStopsNewLegacyInvitesWhileDisabledAndPreservesOldPromises(t *testing.T) {
	s, pool, _ := fixtureConfigured(t, false)
	ctx := context.Background()
	// Use the live PostgreSQL clock shared with migrations. Docker's VM clock
	// may differ from the Windows host; neither timestamp is a fake test clock.
	s.cfg.Now = func() time.Time {
		var now time.Time
		if err := pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			t.Fatal(err)
		}
		return now
	}
	p, err := s.ReferralPolicy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p.Enabled || p.MaxRewardCredits != 0 || p.TotalBudgetCredits != 0 || p.AncillaryCostPPM != nil {
		t.Fatalf("migration activated unreviewed campaign: %+v", p)
	}
	referralOrderFixture(t, s, 100, 2, "subscription", "legacy")
	reserveReferral(t, s, 100)
	fulfillReferral(t, s, 100, 2, "subscription")
	sum, err := s.ResetOpportunities(ctx, 1)
	if err != nil || sum.EarnedTotal != 0 || sum.AvailableCount != 0 {
		t.Fatalf("disabled campaign minted new legacy refresh: %+v %v", sum, err)
	}
	if p.EffectiveAt == nil || p.EffectiveAt.After(s.now()) {
		t.Fatalf("migration did not persist canonical cutover: %+v", p)
	}
	cutover := *p.EffectiveAt
	var eligible bool
	if err = pool.QueryRow(ctx, `SELECT (referral_terms->>'legacy_reset_eligible')::boolean FROM v3_commerce.orders WHERE id=100`).Scan(&eligible); err != nil || eligible {
		t.Fatalf("new order promise=%v err=%v", eligible, err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO v3_identity.users(id,username,inviter_id) VALUES(4,'old-frozen',1),(5,'old-historical',1)`); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{4, 5} {
		order := id + 100
		referralOrderFixture(t, s, order, id, "subscription", "legacy")
		if _, err = pool.Exec(ctx, `UPDATE v3_commerce.orders SET created_at=$2 WHERE id=$1`, order, cutover.Add(-time.Minute)); err != nil {
			t.Fatal(err)
		}
		if id == 4 {
			reserveReferral(t, s, order)
		}
		fulfillReferral(t, s, order, id, "subscription")
	}
	sum, err = s.ResetOpportunities(ctx, 1)
	if err != nil || sum.EarnedTotal != 2 || sum.AvailableCount != 2 {
		t.Fatalf("old frozen/historical promises lost: %+v %v", sum, err)
	}
	for _, enabled := range []bool{true, false} {
		p.Enabled = enabled
		p.EffectiveAt = nil
		zero := int64(0)
		p.AncillaryCostPPM = &zero
		p.MaxRewardCredits = 1000000
		p.TotalBudgetCredits = 2000000
		p, err = s.UpdateReferralPolicy(ctx, p)
		if err != nil || p.EffectiveAt == nil || !p.EffectiveAt.Equal(cutover) {
			t.Fatalf("activation changed migration cutover: %+v %v", p, err)
		}
	}
	view, err := s.ReferralRewards(ctx, 1)
	if err != nil || view.NewInvitesGrantRefresh {
		t.Fatalf("disabled campaign advertised new resets: %+v %v", view, err)
	}
}
