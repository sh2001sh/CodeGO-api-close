package commerce

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type EditSubscription struct {
	StartsAt      time.Time     `json:"starts_at"`
	ExpiresAt     time.Time     `json:"expires_at"`
	State         string        `json:"state"`
	TotalCredits  credits.Micro `json:"total_credits"`
	UsedCredits   credits.Micro `json:"used_credits"`
	PeriodCredits credits.Micro `json:"period_credits"`
	PeriodUsed    credits.Micro `json:"period_used"`
	RequestID     string        `json:"request_id"`
}

func (s *Service) UpdateSubscription(ctx context.Context, id, actor int64, in EditSubscription) error {
	if id <= 0 || actor <= 0 || !validOperation(in.RequestID) || in.StartsAt.IsZero() || !in.ExpiresAt.After(in.StartsAt) || in.TotalCredits < 0 || in.UsedCredits < 0 || (in.TotalCredits > 0 && in.UsedCredits > in.TotalCredits) || in.PeriodCredits < 0 || in.PeriodUsed < 0 || (in.PeriodCredits > 0 && in.PeriodUsed > in.PeriodCredits) || (in.TotalCredits == 0 && in.PeriodCredits == 0) {
		return ErrInvalid
	}
	if in.State != "active" && in.State != "expired" && in.State != "canceled" {
		return ErrInvalid
	}
	key := "admin-edit:" + in.RequestID
	if err := s.queueSubscriptionOperation(ctx, key, id, actor, "update", in); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockSubscriptionUserTx(ctx, tx, id); err != nil {
			return err
		}
		var account, user int64
		var rule, version string
		var converted *time.Time
		var seconds int64
		err := tx.QueryRow(ctx, `SELECT account_id,user_id,reset_period,reset_custom_seconds,policy_version,converted_at FROM v3_commerce.subscriptions WHERE id=$1 FOR UPDATE`, id).Scan(&account, &user, &rule, &seconds, &version, &converted)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if converted != nil || version == PolicyStandardV2 {
			return ErrStateConflict
		}
		if err = s.checkPackagePending(ctx, tx, id); err != nil {
			return err
		}
		done, err := resolveEditSubscriptionOperationTx(ctx, tx, key, id, actor)
		if done || err != nil {
			return err
		}
		if s.cfg.FundingDrain != nil {
			drained, err := s.cfg.FundingDrain.FreezeAndDrained(ctx, tx, account)
			if err != nil {
				return err
			}
			if !drained {
				return ErrFundingPending
			}
		}
		return s.applySubscriptionEditTx(ctx, tx, id, user, account, rule, seconds, key, in)
	})
}

// resolveEditSubscriptionOperationTx returns true (done, nil) if this
// request ID already completed against the same subscription and actor.
func resolveEditSubscriptionOperationTx(ctx context.Context, tx pgx.Tx, key string, id, actor int64) (bool, error) {
	var prior, priorActor int64
	err := tx.QueryRow(ctx, `SELECT subscription_id,actor_id FROM v3_commerce.subscription_operations WHERE operation_id=$1 AND state='completed'`, key).Scan(&prior, &priorActor)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	if prior != id || priorActor != actor {
		return true, ErrStateConflict
	}
	return true, nil
}

// applySubscriptionEditTx rotates the credit bucket to the admin-specified
// grant and rewrites the subscription row to the new schedule and state.
func (s *Service) applySubscriptionEditTx(ctx context.Context, tx pgx.Tx, id, user, account int64, rule string, seconds int64, key string, in EditSubscription) error {
	grant := in.TotalCredits - in.UsedCredits
	if in.TotalCredits == 0 {
		grant = in.PeriodCredits - in.PeriodUsed
	}
	if in.PeriodCredits > 0 {
		grant = min(grant, in.PeriodCredits-in.PeriodUsed)
	}
	if in.State != "active" || !in.ExpiresAt.After(s.cfg.Now()) {
		grant = 0
	}
	newAccount, err := s.rotateSubscriptionBucket(ctx, tx, id, user, account, grant, key, s.cfg.Now())
	if err != nil {
		return err
	}
	next := nextReset(s.cfg.Now(), rule, seconds, in.ExpiresAt)
	_, err = tx.Exec(ctx, `UPDATE v3_commerce.subscriptions SET account_id=$2,starts_at=$3,expires_at=$4,state=$5,
	 total_credits=$6,used_credits=$7,period_credits=$8,period_used=$9,last_reset_at=$10,next_reset_at=$11,deleted_at=NULL,
	 ended_at=CASE WHEN $5='active' THEN NULL ELSE $10::timestamptz END WHERE id=$1`, id, newAccount, in.StartsAt, in.ExpiresAt, in.State,
		int64(in.TotalCredits), int64(in.UsedCredits), int64(in.PeriodCredits), int64(in.PeriodUsed), s.cfg.Now(), next)
	if err != nil {
		return err
	}
	if in.State == "active" && in.ExpiresAt.After(s.cfg.Now()) {
		err = ReapplySubscriptionGroupTx(ctx, tx, id, s.cfg.Now())
	} else {
		err = RestoreSubscriptionGroupTx(ctx, tx, id, s.cfg.Now())
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE v3_commerce.subscription_operations SET state='completed' WHERE operation_id=$1`, key)
	return err
}

func (s *Service) DeleteSubscription(ctx context.Context, id int64) error {
	return s.EndSubscription(ctx, id, 0, true)
}
