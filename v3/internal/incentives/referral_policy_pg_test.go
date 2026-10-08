//go:build pgintegration

package incentives

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestReferralPolicyRealClockActivationAndNoRetroactivePromiseCutoff(t *testing.T) {
	s, _, _ := fixtureConfigured(t, false)
	ctx := context.Background()
	s.cfg.Now = time.Now
	p, err := s.ReferralPolicy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p.EffectiveAt == nil {
		t.Fatal("migration must persist the cutover before campaign activation")
	}
	cutover := *p.EffectiveAt
	zero := int64(0)
	p.Enabled = true
	p.AncillaryCostPPM = &zero
	p.MaxRewardCredits = 1000000
	p.TotalBudgetCredits = 10000000
	before := time.Now()
	p, err = s.UpdateReferralPolicy(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if p.EffectiveAt == nil || !p.EffectiveAt.Equal(cutover) {
		t.Fatalf("bad real-clock cutover %+v", p)
	}
	p.EffectiveAt = &before
	if _, err = s.UpdateReferralPolicy(ctx, p); !errors.Is(err, ErrReferralConflict) {
		t.Fatalf("retroactively changed promises: %v", err)
	}
}
func TestReferralPolicyCannotEnableUnknownAncillaryCostsOrBackdateCutover(t *testing.T) {
	s, _, _ := fixture(t)
	ctx := context.Background()
	p, err := s.ReferralPolicy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p.Enabled = true
	p.MaxRewardCredits = 1000000
	p.TotalBudgetCredits = 10000000
	if _, err = s.UpdateReferralPolicy(ctx, p); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown cost enabled: %v", err)
	}
	zero := int64(0)
	p.AncillaryCostPPM = &zero
	past := s.now().Add(-time.Hour)
	p.EffectiveAt = &past
	if _, err = s.UpdateReferralPolicy(ctx, p); !errors.Is(err, ErrReferralConflict) {
		t.Fatalf("retroactive cutover allowed: %v", err)
	}
}
