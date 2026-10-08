package commerce

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func (s *Service) ConfirmResetCards(ctx context.Context, user int64, quote, request string, accepted bool) (ResetCardExchange, error) {
	out := ResetCardExchange{RequestID: request, QuoteID: quote, Cards: []BoundSubscriptionCard{}}
	if user <= 0 || !validOperation(quote) || !validOperation(request) || !accepted {
		return out, ErrInvalid
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var locked int64
		if err := tx.QueryRow(ctx, `SELECT id FROM v3_identity.users WHERE id=$1 FOR UPDATE`, user).Scan(&locked); err != nil {
			return err
		}
		var priorUser int64
		var priorQuote string
		err := tx.QueryRow(ctx, `SELECT user_id,quote_id,quantity FROM v3_commerce.reset_card_exchanges WHERE request_id=$1`, request).Scan(&priorUser, &priorQuote, &out.Quantity)
		if err == nil {
			if priorUser != user || priorQuote != quote {
				return ErrStateConflict
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var rule, revision, available int64
		var quantity int
		var p Plan
		var cost, increment credits.Micro
		var valid bool
		err = tx.QueryRow(ctx, `SELECT rule_id,rule_revision,quantity,available_snapshot,plan_snapshot,total_cost,incremental_cost,expires_at>$3 FROM v3_commerce.reset_card_quotes WHERE quote_id=$1 AND user_id=$2`, quote, user, s.cfg.Now()).Scan(&rule, &revision, &quantity, &available, &p, &cost, &increment, &valid)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if !valid {
			return ErrStateConflict
		}
		var current, exchanged int64
		if err = tx.QueryRow(ctx, `SELECT available_total,exchanged_total FROM v3_commerce.subscription_reset_opportunity_accounts WHERE user_id=$1 FOR UPDATE`, user).Scan(&current, &exchanged); err != nil {
			return err
		}
		if current != available || current < int64(quantity) || exchanged > math.MaxInt64-int64(quantity) {
			return ErrStateConflict
		}
		r, err := scanCardRule(tx.QueryRow(ctx, `SELECT `+cardRuleColumns+` FROM v3_commerce.reset_card_rules WHERE id=$1 FOR UPDATE`, rule))
		if err != nil {
			return err
		}
		if !r.Enabled || !r.Reviewed || r.Revision != revision || cost > r.BudgetTotal-r.BudgetReserved || increment > r.IncrementalBudgetTotal-r.IncrementalReserved {
			return ErrStateConflict
		}
		if _, err = tx.Exec(ctx, `UPDATE v3_commerce.reset_card_rules SET budget_reserved=budget_reserved+$2,incremental_reserved=incremental_reserved+$3 WHERE id=$1`, rule, cost, increment); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO v3_commerce.reset_card_exchanges(request_id,quote_id,user_id,rule_id,quantity,total_cost,incremental_cost,terms_version,accepted_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, request, quote, user, rule, quantity, cost, increment, ResetCardTerms, s.cfg.Now()); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE v3_commerce.subscription_reset_opportunity_accounts SET available_total=available_total-$2,exchanged_total=exchanged_total+$2,updated_at=$3 WHERE user_id=$1`, user, quantity, s.cfg.Now()); err != nil {
			return err
		}
		for i := 0; i < quantity; i++ {
			if _, err = tx.Exec(ctx, `INSERT INTO v3_commerce.bound_subscription_cards(user_id,exchange_request_id,ordinal,plan_snapshot) VALUES($1,$2,$3,$4)`, user, request, i, p); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO v3_commerce.subscription_reset_opportunity_ledgers(user_id,change_type,delta,balance_after,source_type,source_ref,event_key,note,created_at,updated_at) VALUES($1,'exchange',-1,$2,'reset_card_exchange',$3,$4,'自愿将历史刷新次数兑换为固定额度套餐卡，不能刷新',$5,$5)`, user, current-int64(i)-1, request, fmt.Sprintf("reset-card-exchange:%s:%d", request, i), s.cfg.Now()); err != nil {
				return err
			}
		}
		out.Quantity = quantity
		return nil
	})
	if err != nil {
		return out, err
	}
	out.Cards, err = s.listBoundCards(ctx, user, request)
	if err != nil {
		return out, err
	}
	err = s.pool.QueryRow(ctx, `SELECT available_total FROM v3_commerce.subscription_reset_opportunity_accounts WHERE user_id=$1`, user).Scan(&out.RemainingCount)
	return out, err
}
