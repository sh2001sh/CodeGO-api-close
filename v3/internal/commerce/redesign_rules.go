package commerce

import (
	"context"
	"encoding/hex"
	"strings"

	"github.com/jackc/pgx/v5"
)

const conversionRuleColumns = `id,plan_id,basis_key,source_credits,wallet_credits,paid_wallet_credits,recognized_revenue_credits,refreshed_paid_wallet_credits,enabled,reviewed,revision,note`
const cardRuleColumns = `id,name,reference_plan_id,card_plan_id,credits,cost_per_card,baseline_cost,budget_total,budget_reserved,incremental_budget_total,incremental_reserved,enabled,reviewed,revision,note`

func scanConversionRule(row scanner) (ConversionRule, error) {
	var r ConversionRule
	err := row.Scan(&r.ID, &r.PlanID, &r.BasisKey, &r.SourceCredits, &r.WalletCredits, &r.PaidWalletCredits, &r.RecognizedRevenueCredits, &r.RefreshedPaidWalletCredits, &r.Enabled, &r.Reviewed, &r.Revision, &r.Note)
	return r, err
}
func scanCardRule(row scanner) (ResetCardRule, error) {
	var r ResetCardRule
	err := row.Scan(&r.ID, &r.Name, &r.ReferencePlanID, &r.CardPlanID, &r.Credits, &r.CostPerCard, &r.BaselineCost, &r.BudgetTotal, &r.BudgetReserved, &r.IncrementalBudgetTotal, &r.IncrementalReserved, &r.Enabled, &r.Reviewed, &r.Revision, &r.Note)
	return r, err
}

func (s *Service) RedesignRules(ctx context.Context) (RedesignRules, error) {
	result := RedesignRules{ConversionRules: []ConversionRule{}, CardRules: []ResetCardRule{}}
	rows, err := s.pool.Query(ctx, `SELECT `+conversionRuleColumns+` FROM v3_commerce.subscription_conversion_rules ORDER BY id`)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		r, e := scanConversionRule(rows)
		if e != nil {
			rows.Close()
			return result, e
		}
		result.ConversionRules = append(result.ConversionRules, r)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return result, err
	}
	rows, err = s.pool.Query(ctx, `SELECT `+cardRuleColumns+` FROM v3_commerce.reset_card_rules ORDER BY id`)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		r, e := scanCardRule(rows)
		if e != nil {
			return result, e
		}
		result.CardRules = append(result.CardRules, r)
	}
	return result, rows.Err()
}

func (s *Service) SaveRedesignRules(ctx context.Context, in RedesignRules) (RedesignRules, error) {
	if len(in.ConversionRules) > 100 || len(in.CardRules) > 100 {
		return in, ErrInvalid
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for _, r := range in.ConversionRules {
			if err := s.saveConversionRuleTx(ctx, tx, r); err != nil {
				return err
			}
		}
		for _, r := range in.CardRules {
			if err := s.saveCardRuleTx(ctx, tx, r); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return in, err
	}
	return s.RedesignRules(ctx)
}

func (s *Service) saveConversionRuleTx(ctx context.Context, tx pgx.Tx, r ConversionRule) error {
	raw, e := hex.DecodeString(r.BasisKey)
	if e != nil || len(raw) != 32 || r.PlanID <= 0 || r.SourceCredits <= 0 || r.WalletCredits <= 0 || r.PaidWalletCredits < 0 || r.PaidWalletCredits > r.WalletCredits || len(r.Note) > 2000 || (r.Enabled && !r.Reviewed) || (r.RefreshedPaidWalletCredits != nil && (*r.RefreshedPaidWalletCredits < 0 || *r.RefreshedPaidWalletCredits > r.WalletCredits)) {
		return ErrInvalid
	}
	var version string
	if err := tx.QueryRow(ctx, `SELECT policy_version FROM v3_commerce.plans WHERE id=$1 FOR SHARE`, r.PlanID).Scan(&version); err != nil {
		return err
	}
	if version != PolicyLegacy {
		return ErrInvalid
	}
	if r.ID == 0 {
		_, err := tx.Exec(ctx, `INSERT INTO v3_commerce.subscription_conversion_rules(plan_id,basis_key,source_credits,wallet_credits,paid_wallet_credits,recognized_revenue_credits,refreshed_paid_wallet_credits,enabled,reviewed,note) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, r.PlanID, r.BasisKey, r.SourceCredits, r.WalletCredits, r.PaidWalletCredits, r.RecognizedRevenueCredits, r.RefreshedPaidWalletCredits, r.Enabled, r.Reviewed, r.Note)
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE v3_commerce.subscription_conversion_rules SET wallet_credits=$2,paid_wallet_credits=$3,refreshed_paid_wallet_credits=$4,enabled=$5,reviewed=$6,note=$7,recognized_revenue_credits=$13,revision=revision+1,updated_at=$8 WHERE id=$1 AND revision=$9 AND plan_id=$10 AND source_credits=$11 AND basis_key=$12`, r.ID, r.WalletCredits, r.PaidWalletCredits, r.RefreshedPaidWalletCredits, r.Enabled, r.Reviewed, r.Note, s.cfg.Now(), r.Revision, r.PlanID, r.SourceCredits, r.BasisKey, r.RecognizedRevenueCredits)
	if err == nil && tag.RowsAffected() != 1 {
		return ErrStateConflict
	}
	return err
}

func (s *Service) saveCardRuleTx(ctx context.Context, tx pgx.Tx, r ResetCardRule) error {
	if strings.TrimSpace(r.Name) == "" || len(r.Name) > 200 || r.ReferencePlanID <= 0 || r.CardPlanID <= 0 || r.Credits <= 0 || r.CostPerCard < 0 || r.BaselineCost < 0 || r.BudgetTotal < 0 || r.IncrementalBudgetTotal < 0 || len(r.Note) > 2000 || (r.Enabled && (!r.Reviewed || r.CostPerCard == 0)) {
		return ErrInvalid
	}
	p, err := scanPlan(tx.QueryRow(ctx, `SELECT `+planColumns+` FROM v3_commerce.plans WHERE id=$1 FOR SHARE`, r.CardPlanID))
	if err != nil {
		return err
	}
	if p.PolicyVersion != PolicyStandardV2 {
		return ErrInvalid
	}
	if r.ID == 0 {
		_, err = tx.Exec(ctx, `INSERT INTO v3_commerce.reset_card_rules(name,reference_plan_id,card_plan_id,credits,cost_per_card,baseline_cost,budget_total,incremental_budget_total,enabled,reviewed,note) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, r.Name, r.ReferencePlanID, r.CardPlanID, r.Credits, r.CostPerCard, r.BaselineCost, r.BudgetTotal, r.IncrementalBudgetTotal, r.Enabled, r.Reviewed, r.Note)
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE v3_commerce.reset_card_rules SET name=$2,reference_plan_id=$3,card_plan_id=$4,credits=$5,cost_per_card=$6,baseline_cost=$7,budget_total=$8,incremental_budget_total=$9,enabled=$10,reviewed=$11,note=$12,revision=revision+1,updated_at=$13 WHERE id=$1 AND revision=$14 AND budget_reserved<=$8 AND incremental_reserved<=$9`, r.ID, r.Name, r.ReferencePlanID, r.CardPlanID, r.Credits, r.CostPerCard, r.BaselineCost, r.BudgetTotal, r.IncrementalBudgetTotal, r.Enabled, r.Reviewed, r.Note, s.cfg.Now(), r.Revision)
	if err == nil && tag.RowsAffected() != 1 {
		return ErrStateConflict
	}
	return err
}

func (s *Service) RedesignPreview(ctx context.Context) (RedesignCostPreview, error) {
	var out RedesignCostPreview
	err := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM v3_commerce.subscriptions WHERE policy_version='legacy' AND state='active' AND expires_at>$1),(SELECT COALESCE(sum(available_total),0)::bigint FROM v3_commerce.subscription_reset_opportunity_accounts)`, s.cfg.Now()).Scan(&out.ActiveLegacyCount, &out.AvailableResetCount)
	if err != nil {
		return out, err
	}
	out.Rules, err = s.RedesignRules(ctx)
	if err != nil {
		return out, err
	}
	rows, err := s.pool.Query(ctx, `SELECT user_id,id FROM v3_commerce.subscriptions WHERE policy_version='legacy' AND state='active' AND expires_at>$1 ORDER BY id LIMIT 100`, s.cfg.Now())
	if err != nil {
		return out, err
	}
	pairs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) ([2]int64, error) { var p [2]int64; e := r.Scan(&p[0], &p[1]); return p, e })
	if err != nil {
		return out, err
	}
	out.Candidates = []WalletConversionQuote{}
	for _, p := range pairs {
		q, e := s.previewWalletConversion(ctx, p[0], p[1], false)
		if e != nil {
			return out, e
		}
		out.Candidates = append(out.Candidates, q)
	}
	return out, nil
}
