package commerce

import (
	"context"

	"github.com/jackc/pgx/v5"
)

const (
	PolicyLegacy     = "legacy"
	PolicyStandardV2 = "standard_v2"
)

func policyVersion(value string) string {
	if value == "" {
		return PolicyLegacy
	}
	return value
}

func normalizePlanPolicy(p *Plan) error {
	p.PolicyVersion = policyVersion(p.PolicyVersion)
	if p.PolicyVersion != PolicyLegacy && p.PolicyVersion != PolicyStandardV2 {
		return ErrInvalid
	}
	if p.PolicyVersion == PolicyStandardV2 && (p.Credits <= 0 || p.PeriodCredits != 0 || (p.ResetPeriod != "" && p.ResetPeriod != "never") || p.ResetCustomSeconds != 0 || p.GroupBuyEnabled || p.GroupBuyBonus != 0 || p.GroupBuyBonus2 != 0 || p.GroupBuyBonus3 != 0 || p.GroupBuyBonus5 != 0 || p.FuelEnabled) {
		return ErrInvalid
	}
	return nil
}

func (s *Service) validatePlanVersionUpdate(ctx context.Context, p Plan) error {
	if p.ID <= 0 {
		return nil
	}
	var changed, issued bool
	err := s.pool.QueryRow(ctx, `SELECT policy_version<>$2,EXISTS(SELECT 1 FROM v3_commerce.orders WHERE plan_id=$1) OR EXISTS(SELECT 1 FROM v3_commerce.subscriptions WHERE plan_id=$1) OR EXISTS(SELECT 1 FROM v3_commerce.redemption_codes WHERE plan_id=$1) FROM v3_commerce.plans WHERE id=$1`, p.ID, p.PolicyVersion).Scan(&changed, &issued)
	if err == pgx.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if changed && issued {
		return ErrStateConflict
	}
	return nil
}

// Use the frozen specification when present. Legacy imports without snapshots
// retain their legacy semantics and are never inferred to be a newer policy.
func subscriptionPlan(ctx context.Context, q rowQuerierCommerce, id int64) (Plan, error) {
	var p Plan
	var plan int64
	err := q.QueryRow(ctx, `SELECT plan_id,plan_snapshot FROM v3_commerce.subscriptions WHERE id=$1`, id).Scan(&plan, &p)
	if err != nil {
		return p, err
	}
	if p.ID > 0 {
		return p, nil
	}
	return scanPlan(q.QueryRow(ctx, `SELECT `+planColumns+` FROM v3_commerce.plans WHERE id=$1`, plan))
}

type rowQuerierCommerce interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}
