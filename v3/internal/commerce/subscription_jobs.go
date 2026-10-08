package commerce

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Persist before closing Redis admission, since a caller may leave while an
// existing stream is draining. Workers finish the same authorized mutation.
func (s *Service) queueSubscriptionOperation(ctx context.Context, key string, id, actor int64, kind string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var found int64
		// Take the actor FK lock before the subscription lock. A paid checkout
		// locks its user first; taking this FK lock later would reverse that order.
		if err := tx.QueryRow(ctx, `SELECT id FROM v3_identity.users WHERE id=$1 FOR KEY SHARE`, actor).Scan(&found); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT id FROM v3_commerce.subscriptions WHERE id=$1 FOR UPDATE`, id).Scan(&found); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		var identical bool
		err = tx.QueryRow(ctx, `INSERT INTO v3_commerce.subscription_operations(operation_id,subscription_id,actor_id,kind,payload)
	 SELECT $1,id,$3,$4,$5::jsonb FROM v3_commerce.subscriptions WHERE id=$2
	 AND NOT EXISTS(SELECT 1 FROM v3_commerce.subscription_operations op WHERE op.subscription_id=$2 AND op.state='pending'
	 AND op.kind IN ('conversion','invalidate','delete') AND op.operation_id<>$1)
	 ON CONFLICT(operation_id) DO UPDATE SET operation_id=EXCLUDED.operation_id
	 RETURNING subscription_id=$2 AND actor_id=$3 AND kind=$4 AND payload=$5::jsonb`, key, id, actor, kind, string(raw)).Scan(&identical)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrFundingPending
		}
		if err != nil {
			return err
		}
		if !identical {
			return ErrStateConflict
		}
		return nil
	})
}

func (s *Service) RecoverSubscriptionChanges(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `SELECT operation_id,subscription_id,actor_id,kind,payload
	 FROM v3_commerce.subscription_operations WHERE state='pending' ORDER BY created_at,operation_id LIMIT $1`, limit)
	if err != nil {
		return 0, err
	}
	type job struct {
		key, kind string
		id, actor int64
		payload   []byte
	}
	var pending []job
	for rows.Next() {
		var row job
		if err = rows.Scan(&row.key, &row.id, &row.actor, &row.kind, &row.payload); err != nil {
			rows.Close()
			return 0, err
		}
		pending = append(pending, row)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return 0, err
	}
	count := 0
	for _, row := range pending {
		err := s.replaySubscriptionOperation(ctx, row.key, row.id, row.actor, row.kind, row.payload)
		if errors.Is(err, ErrFundingPending) {
			continue
		}
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrStateConflict) || errors.Is(err, ErrInvalid) {
			if _, saveErr := s.pool.Exec(ctx, `UPDATE v3_commerce.subscription_operations SET state='failed' WHERE operation_id=$1 AND state='pending'`, row.key); saveErr != nil {
				return count, saveErr
			}
			continue
		}
		if err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

// replaySubscriptionOperation dispatches a single queued operation to its
// handler based on kind, decoding the stored payload into the matching
// request shape.
func (s *Service) replaySubscriptionOperation(ctx context.Context, key string, id, actor int64, kind string, payload []byte) error {
	switch kind {
	case "reset":
		var request struct {
			RequestID string `json:"request_id"`
		}
		if err := json.Unmarshal(payload, &request); err != nil {
			return err
		}
		return s.ResetSubscription(ctx, id, actor, request.RequestID)
	case "update":
		var request EditSubscription
		if err := json.Unmarshal(payload, &request); err != nil {
			return err
		}
		return s.UpdateSubscription(ctx, id, actor, request)
	case "conversion":
		var request struct {
			RequestID     string `json:"request_id"`
			Percent       int    `json:"percent"`
			WalletQuoteID string `json:"wallet_quote_id"`
			AcceptedTerms bool   `json:"accepted_terms"`
		}
		if err := json.Unmarshal(payload, &request); err != nil {
			return err
		}
		if request.WalletQuoteID != "" {
			_, err := s.ConfirmWalletConversion(ctx, actor, request.WalletQuoteID, request.RequestID, request.AcceptedTerms)
			return err
		}
		_, err := s.ConvertSubscription(ctx, actor, id, request.Percent, request.RequestID)
		return err
	case "invalidate", "delete":
		return s.applySubscriptionEnd(ctx, id, key, kind == "delete")
	default:
		return ErrInvalid
	}
}
