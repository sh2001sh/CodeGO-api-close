package commerce

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (s *Service) SetPlanEnabled(ctx context.Context, id int64, enabled bool) error {
	if id <= 0 {
		return ErrInvalid
	}
	tag, err := s.pool.Exec(ctx, `UPDATE v3_commerce.plans SET enabled=$2 WHERE id=$1`, id, enabled)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// Purchased plans remain available to receipts and history. Deletion disables
// those plans; an unused plan can be removed safely.
func (s *Service) DeletePlan(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrInvalid
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var found int64
		if err := tx.QueryRow(ctx, `SELECT id FROM v3_commerce.plans WHERE id=$1 FOR UPDATE`, id).Scan(&found); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		var used bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_commerce.orders WHERE plan_id=$1)
		 OR EXISTS(SELECT 1 FROM v3_commerce.subscriptions WHERE plan_id=$1)`, id).Scan(&used); err != nil {
			return err
		}
		if used {
			_, err := tx.Exec(ctx, `UPDATE v3_commerce.plans SET enabled=false WHERE id=$1`, id)
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM v3_commerce.plans WHERE id=$1`, id)
		return err
	})
}

func validOperation(operation string) bool {
	return strings.TrimSpace(operation) != "" && len(operation) <= 128
}

// BindSubscription grants a configured plan without payment. Admin routes are
// responsible for authorization; a request key prevents retried credits.
func (s *Service) BindSubscription(ctx context.Context, user, plan int64, operation string) (int64, error) {
	if user <= 0 || plan <= 0 || !validOperation(operation) {
		return 0, ErrInvalid
	}
	key := "admin-bind:" + operation
	var id int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, key); err != nil {
			return err
		}
		var owner int64
		if err := tx.QueryRow(ctx, `SELECT id FROM v3_identity.users WHERE id=$1 FOR UPDATE`, user).Scan(&owner); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		var priorUser, priorPlan int64
		err := tx.QueryRow(ctx, `SELECT id,user_id,plan_id FROM v3_commerce.subscriptions WHERE reward_operation=$1`, key).Scan(&id, &priorUser, &priorPlan)
		if err == nil {
			if user != priorUser || plan != priorPlan {
				return ErrStateConflict
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		p, err := scanPlan(tx.QueryRow(ctx, `SELECT `+planColumns+` FROM v3_commerce.plans WHERE id=$1 AND enabled`, plan))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if p.MaxPurchasePerUser > 0 {
			var count int
			if err = tx.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.subscriptions WHERE user_id=$1 AND plan_id=$2`, user, plan).Scan(&count); err != nil {
				return err
			}
			if count >= p.MaxPurchasePerUser {
				return ErrStateConflict
			}
		}
		o := Order{UserID: user, PlanID: &p.ID, Credits: p.Credits, PeriodSeconds: p.PeriodSeconds, TradeNo: key,
			PeriodCredits: p.PeriodCredits, ResetPeriod: p.ResetPeriod, ResetCustomSeconds: p.ResetCustomSeconds, LegacyPeriodic: p.PeriodCredits == 0 && p.ResetPeriod != "never", DurationUnit: p.DurationUnit, DurationValue: p.DurationValue, CustomSeconds: p.CustomSeconds}
		if err = s.grantSubscription(ctx, tx, o); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT id FROM v3_commerce.subscriptions WHERE reward_operation=$1`, key).Scan(&id)
	})
	return id, err
}

func (s *Service) InvalidateSubscription(ctx context.Context, id int64) error {
	return s.EndSubscription(ctx, id, 0, false)
}

func (s *Service) ResetSubscription(ctx context.Context, id, actor int64, operation string) error {
	if id <= 0 || actor <= 0 || !validOperation(operation) {
		return ErrInvalid
	}
	key := "admin-reset:" + operation
	if err := s.queueSubscriptionOperation(ctx, key, id, actor, "reset", map[string]string{"request_id": operation}); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var found int64
		if err := tx.QueryRow(ctx, `SELECT id FROM v3_commerce.subscriptions WHERE id=$1 FOR UPDATE`, id).Scan(&found); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		var prior, priorActor int64
		err := tx.QueryRow(ctx, `SELECT subscription_id,actor_id FROM v3_commerce.subscription_operations WHERE operation_id=$1 AND state='completed'`, key).Scan(&prior, &priorActor)
		if err == nil {
			if prior != id || priorActor != actor {
				return ErrStateConflict
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err = s.resetSubscriptionTx(ctx, tx, id, true, key); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE v3_commerce.subscription_operations SET state='completed' WHERE operation_id=$1`, key)
		return err
	})
}
