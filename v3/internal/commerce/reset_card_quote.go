package commerce

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type AvailableResetCardRule struct {
	ID              int64         `json:"id"`
	Name            string        `json:"name"`
	ReferencePlanID int64         `json:"reference_plan_id"`
	Credits         credits.Micro `json:"credits"`
	DurationDays    int           `json:"duration_days"`
}

func (s *Service) AvailableResetCardRules(ctx context.Context, user int64) ([]AvailableResetCardRule, error) {
	rows, err := s.pool.Query(ctx, `SELECT r.id,r.name,r.reference_plan_id,r.credits FROM v3_commerce.reset_card_rules r WHERE enabled AND reviewed AND EXISTS(SELECT 1 FROM v3_commerce.subscriptions s WHERE s.user_id=$1 AND s.plan_id=r.reference_plan_id AND s.policy_version='legacy') ORDER BY r.id`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AvailableResetCardRule{}
	for rows.Next() {
		var r AvailableResetCardRule
		r.DurationDays = 90
		if err = rows.Scan(&r.ID, &r.Name, &r.ReferencePlanID, &r.Credits); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Service) QuoteResetCards(ctx context.Context, user, rule int64, quantity int) (ResetCardQuote, error) {
	out := ResetCardQuote{RuleID: rule, Quantity: quantity, TermsVersion: ResetCardTerms}
	if user <= 0 || rule <= 0 || quantity < 1 || quantity > 100 {
		return out, ErrInvalid
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		r, err := scanCardRule(tx.QueryRow(ctx, `SELECT `+cardRuleColumns+` FROM v3_commerce.reset_card_rules WHERE id=$1 AND enabled AND reviewed FOR SHARE`, rule))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		var eligible bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_commerce.subscriptions WHERE user_id=$1 AND plan_id=$2 AND policy_version='legacy')`, user, r.ReferencePlanID).Scan(&eligible); err != nil {
			return err
		}
		if !eligible {
			return ErrNotFound
		}
		err = tx.QueryRow(ctx, `SELECT available_total FROM v3_commerce.subscription_reset_opportunity_accounts WHERE user_id=$1`, user).Scan(&out.AvailableCount)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrStateConflict
		}
		if err != nil {
			return err
		}
		if out.AvailableCount < int64(quantity) {
			return ErrStateConflict
		}
		p, err := scanPlan(tx.QueryRow(ctx, `SELECT `+planColumns+` FROM v3_commerce.plans WHERE id=$1 FOR SHARE`, r.CardPlanID))
		if err != nil {
			return err
		}
		if p.PolicyVersion != PolicyStandardV2 {
			return ErrStateConflict
		}
		p.Credits, p.PeriodCredits = r.Credits, 0
		p.Name = r.Name
		p.PlanType = "fixed"
		p.DurationUnit, p.DurationValue, p.CustomSeconds, p.PeriodSeconds = "day", 90, 0, 90*86400
		p.ResetPeriod, p.ResetCustomSeconds = "never", 0
		p.UpgradeGroup, p.MembershipTier = "", "none"
		p.LuckyDrawEnabled = false
		p.MaxPurchasePerUser = 0
		out.Plan = p
		out.RuleRevision = r.Revision
		out.ExpiresAt = s.cfg.Now().Add(5 * time.Minute)
		out.QuoteID, err = tradeNumber()
		if err != nil {
			return err
		}
		cost, err := floorCreditRatio(credits.Micro(quantity), r.CostPerCard, 1)
		if err != nil {
			return err
		}
		increment, err := floorCreditRatio(credits.Micro(quantity), max(r.CostPerCard-r.BaselineCost, 0), 1)
		if err != nil {
			return err
		}
		if cost > r.BudgetTotal-r.BudgetReserved || increment > r.IncrementalBudgetTotal-r.IncrementalReserved {
			return ErrStateConflict
		}
		_, err = tx.Exec(ctx, `INSERT INTO v3_commerce.reset_card_quotes(quote_id,user_id,rule_id,rule_revision,quantity,available_snapshot,plan_snapshot,total_cost,incremental_cost,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, out.QuoteID, user, rule, r.Revision, quantity, out.AvailableCount, p, cost, increment, out.ExpiresAt)
		return err
	})
	return out, err
}
