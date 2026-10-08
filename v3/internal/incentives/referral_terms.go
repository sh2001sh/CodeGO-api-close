package incentives

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

func referralPublicTerms(p ReferralPolicy, reason string) ReferralTerms {
	return ReferralTerms{Eligible: reason == "", Reason: reason, Version: "consumption_v1", PolicyRevision: p.Revision, RewardPPM: p.RewardPPM, ProfitSharePPM: p.ProfitSharePPM, WindowDays: p.WindowDays, DelayDays: p.DelayDays, MaxRewardCredits: p.MaxRewardCredits, OwnerOnly: true, NoRefresh: true}
}
func (s *Service) legacyResetAllowed(ctx context.Context, tx pgx.Tx, o referralOrder) (bool, error) {
	var terms struct {
		Eligible *bool `json:"legacy_reset_eligible"`
	}
	if err := json.Unmarshal(o.terms, &terms); err != nil {
		return false, err
	}
	if terms.Eligible != nil {
		return *terms.Eligible, nil
	}
	p, err := scanReferralPolicy(tx.QueryRow(ctx, `SELECT `+referralPolicyColumns+` FROM v3_commerce.referral_consumption_policy WHERE id`))
	if err != nil {
		return false, err
	}
	return p.EffectiveAt == nil || o.createdAt.Before(*p.EffectiveAt), nil
}
