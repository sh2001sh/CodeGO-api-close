package incentives

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type ReferralPolicy struct {
	Enabled            bool          `json:"enabled"`
	Revision           int64         `json:"revision"`
	EffectiveAt        *time.Time    `json:"effective_at"`
	AncillaryCostPPM   *int64        `json:"ancillary_cost_ppm"`
	RewardPPM          int64         `json:"reward_ppm"`
	ProfitSharePPM     int64         `json:"profit_share_ppm"`
	WindowDays         int           `json:"window_days"`
	DelayDays          int           `json:"delay_days"`
	MaxRewardCredits   credits.Micro `json:"max_reward_credits"`
	TotalBudgetCredits credits.Micro `json:"total_budget_credits"`
	ReservedCredits    credits.Micro `json:"reserved_credits"`
	SpentCredits       credits.Micro `json:"spent_credits"`
}

const referralPolicyColumns = `enabled,revision,effective_at,ancillary_cost_ppm,reward_ppm,profit_share_ppm,window_days,delay_days,max_reward_credits,total_budget_credits,reserved_credits,spent_credits`

func scanReferralPolicy(row pgx.Row) (ReferralPolicy, error) {
	var p ReferralPolicy
	err := row.Scan(&p.Enabled, &p.Revision, &p.EffectiveAt, &p.AncillaryCostPPM, &p.RewardPPM, &p.ProfitSharePPM, &p.WindowDays, &p.DelayDays, &p.MaxRewardCredits, &p.TotalBudgetCredits, &p.ReservedCredits, &p.SpentCredits)
	return p, err
}
func (s *Service) ReferralPolicy(ctx context.Context) (ReferralPolicy, error) {
	return scanReferralPolicy(s.pool.QueryRow(ctx, `SELECT `+referralPolicyColumns+` FROM v3_commerce.referral_consumption_policy WHERE id`))
}
func (s *Service) UpdateReferralPolicy(ctx context.Context, p ReferralPolicy) (ReferralPolicy, error) {
	if p.Revision <= 0 || p.RewardPPM < 0 || p.RewardPPM > 10000 || p.ProfitSharePPM < 0 || p.ProfitSharePPM > 200000 || p.WindowDays < 1 || p.WindowDays > 30 || p.DelayDays < 7 || p.DelayDays > 90 || p.MaxRewardCredits < 0 || p.TotalBudgetCredits < 0 || (p.AncillaryCostPPM != nil && (*p.AncillaryCostPPM < 0 || *p.AncillaryCostPPM > 1000000)) || (p.Enabled && (p.MaxRewardCredits == 0 || p.TotalBudgetCredits == 0 || p.AncillaryCostPPM == nil)) {
		return ReferralPolicy{}, ErrInvalid
	}
	var result ReferralPolicy
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		current, err := scanReferralPolicy(tx.QueryRow(ctx, `SELECT `+referralPolicyColumns+` FROM v3_commerce.referral_consumption_policy WHERE id FOR UPDATE`))
		if err != nil {
			return err
		}
		committed, err := current.ReservedCredits.Add(current.SpentCredits)
		if err != nil {
			return err
		}
		if current.Revision != p.Revision || p.TotalBudgetCredits < committed {
			return ErrReferralConflict
		}
		activationNow := s.now()
		if current.EffectiveAt != nil {
			if p.EffectiveAt != nil && !p.EffectiveAt.Equal(*current.EffectiveAt) {
				return ErrReferralConflict
			}
			p.EffectiveAt = current.EffectiveAt
		} else if p.Enabled && p.EffectiveAt == nil {
			p.EffectiveAt = &activationNow
		}
		if current.EffectiveAt == nil && p.EffectiveAt != nil && p.EffectiveAt.Before(activationNow) {
			return ErrReferralConflict
		}
		result, err = scanReferralPolicy(tx.QueryRow(ctx, `UPDATE v3_commerce.referral_consumption_policy SET enabled=$1,revision=revision+1,reward_ppm=$2,profit_share_ppm=$3,window_days=$4,delay_days=$5,max_reward_credits=$6,total_budget_credits=$7,updated_at=$8,effective_at=$9,ancillary_cost_ppm=$10 WHERE id RETURNING `+referralPolicyColumns, p.Enabled, p.RewardPPM, p.ProfitSharePPM, p.WindowDays, p.DelayDays, p.MaxRewardCredits, p.TotalBudgetCredits, s.now(), p.EffectiveAt, p.AncillaryCostPPM))
		return err
	})
	return result, err
}
