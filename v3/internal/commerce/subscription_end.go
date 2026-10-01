package commerce

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
)

// EndSubscription persists the administrator's action before the Redis gate
// closes. A worker finishes cancellation/deletion after an existing stream.
func (s *Service) EndSubscription(ctx context.Context, id, actor int64, deleted bool) error {
	if id <= 0 {
		return ErrInvalid
	}
	var account, user int64
	err := s.pool.QueryRow(ctx, `SELECT account_id,user_id FROM v3_commerce.subscriptions WHERE id=$1`, id).Scan(&account, &user)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if actor == 0 {
		actor = user
	}
	kind := "invalidate"
	if deleted {
		kind = "delete"
	}
	key := "admin-end:" + strconv.FormatInt(id, 10) + ":" + strconv.FormatInt(account, 10) + ":" + kind
	if err = s.queueSubscriptionOperation(ctx, key, id, actor, kind, map[string]any{"account_id": account}); err != nil {
		return err
	}
	return s.applySubscriptionEnd(ctx, id, key, deleted)
}

func (s *Service) applySubscriptionEnd(ctx context.Context, id int64, key string, deleted bool) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockSubscriptionUserTx(ctx, tx, id); err != nil {
			return err
		}
		var account int64
		var state, operationState string
		if err := tx.QueryRow(ctx, `SELECT account_id,state FROM v3_commerce.subscriptions WHERE id=$1 FOR UPDATE`, id).Scan(&account, &state); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT state FROM v3_commerce.subscription_operations WHERE operation_id=$1 FOR UPDATE`, key).Scan(&operationState); err != nil {
			return err
		}
		if operationState == "completed" {
			return nil
		}
		if operationState != "pending" {
			return ErrStateConflict
		}
		var busy bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_commerce.package_checkouts WHERE target_subscription_id=$1 AND state IN ('preparing','checkout'))
   OR EXISTS(SELECT 1 FROM v3_commerce.subscription_operations WHERE subscription_id=$1 AND kind='conversion' AND state='pending')`, id).Scan(&busy); err != nil {
			return err
		}
		if busy {
			return ErrFundingPending
		}
		if state == "active" {
			if err := s.endSubscription(ctx, tx, id, account, "canceled"); err != nil {
				return err
			}
		}
		if deleted {
			if _, err := tx.Exec(ctx, `UPDATE v3_commerce.subscriptions SET deleted_at=$2 WHERE id=$1`, id, s.cfg.Now()); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `UPDATE v3_commerce.subscription_operations SET state='completed' WHERE operation_id=$1`, key)
		return err
	})
}
