package commerce

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func (s *Service) BoundCards(ctx context.Context, user int64) ([]BoundSubscriptionCard, error) {
	return s.listBoundCards(ctx, user, "")
}
func (s *Service) listBoundCards(ctx context.Context, user int64, request string) ([]BoundSubscriptionCard, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,state,plan_snapshot,subscription_id,activated_at FROM v3_commerce.bound_subscription_cards WHERE user_id=$1 AND ($2='' OR exchange_request_id=$2) ORDER BY id DESC`, user, request)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BoundSubscriptionCard{}
	for rows.Next() {
		var c BoundSubscriptionCard
		if err = rows.Scan(&c.ID, &c.State, &c.Plan, &c.SubscriptionID, &c.ActivatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Service) ActivateBoundCard(ctx context.Context, user, id int64, request string) (BoundSubscriptionCard, error) {
	var out BoundSubscriptionCard
	if user <= 0 || id <= 0 || !validOperation(request) {
		return out, ErrInvalid
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var locked int64
		if err := tx.QueryRow(ctx, `SELECT id FROM v3_identity.users WHERE id=$1 FOR UPDATE`, user).Scan(&locked); err != nil {
			return err
		}
		var prior *string
		err := tx.QueryRow(ctx, `SELECT id,state,plan_snapshot,subscription_id,activated_at,activation_request_id FROM v3_commerce.bound_subscription_cards WHERE id=$1 AND user_id=$2 FOR UPDATE`, id, user).Scan(&out.ID, &out.State, &out.Plan, &out.SubscriptionID, &out.ActivatedAt, &prior)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if out.State == "activated" {
			if prior == nil || *prior != request {
				return ErrStateConflict
			}
			return nil
		}
		var used bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_commerce.bound_subscription_cards WHERE activation_request_id=$1)`, request).Scan(&used); err != nil {
			return err
		}
		if used {
			return ErrStateConflict
		}
		p := out.Plan
		if p.PolicyVersion != PolicyStandardV2 || normalizePlanPolicy(&p) != nil {
			return ErrStateConflict
		}
		zero := credits.Micro(0)
		operation := fmt.Sprintf("reset-card-activation:%d", id)
		o := Order{PolicyVersion: PolicyStandardV2, PlanSnapshot: p, RecognizedRevenueCredits: &zero, UserID: user, PlanID: &p.ID, Credits: p.Credits, PeriodSeconds: p.PeriodSeconds, DurationUnit: p.DurationUnit, DurationValue: p.DurationValue, CustomSeconds: p.CustomSeconds, ResetPeriod: "never", TradeNo: operation}
		if err = s.insertNewSubscriptionTx(ctx, tx, o); err != nil {
			return err
		}
		var sub int64
		if err = tx.QueryRow(ctx, `UPDATE v3_commerce.subscriptions SET source='reset_card' WHERE reward_operation=$1 RETURNING id`, operation).Scan(&sub); err != nil {
			return err
		}
		// Use the canonical database timestamp that starts the entitlement,
		// including PostgreSQL's precision, for the first and replayed receipt.
		return tx.QueryRow(ctx, `UPDATE v3_commerce.bound_subscription_cards
		 SET state='activated',activation_request_id=$2,subscription_id=$3,
		 activated_at=(SELECT starts_at FROM v3_commerce.subscriptions WHERE id=$3)
		 WHERE id=$1 RETURNING state,subscription_id,activated_at`, id, request, sub).
			Scan(&out.State, &out.SubscriptionID, &out.ActivatedAt)
	})
	return out, err
}
