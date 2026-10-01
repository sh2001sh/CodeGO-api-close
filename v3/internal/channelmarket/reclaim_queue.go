package channelmarket

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type ReclaimJob struct {
	ID        string        `json:"operation_id"`
	Status    string        `json:"status"`
	Count     int64         `json:"reclaimed_count"`
	Amount    credits.Micro `json:"reclaimed_amount_micro"`
	Batch     int64         `json:"batch_number"`
	Error     string        `json:"error_message,omitempty"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

func (s *Service) QueueReclaim(ctx context.Context, a Actor, r ReclaimRequest) (ReclaimJob, error) {
	var result ReclaimJob
	if !a.Admin {
		return result, ErrNotFound
	}
	if r.OperationID == "" || len(r.OperationID) > 128 || len(r.OwnerIDs) == 0 || len(r.OwnerIDs) > 100 || r.MaxAmount < 0 {
		return result, ErrInvalid
	}
	sort.Slice(r.OwnerIDs, func(i, j int) bool { return r.OwnerIDs[i] < r.OwnerIDs[j] })
	for i, owner := range r.OwnerIDs {
		if owner <= 0 || (i > 0 && owner == r.OwnerIDs[i-1]) {
			return result, ErrInvalid
		}
	}
	body, err := json.Marshal(r)
	if err != nil {
		return result, err
	}
	fingerprint := sha256.Sum256(body)
	err = s.transaction(ctx, func(tx pgx.Tx) error {
		tag, e := tx.Exec(ctx, `INSERT INTO v3_channelmarket.income_reclaims(id,actor_user_id,fingerprint,response,filter,status) VALUES($1,$2,$3,'{"count":0,"amount_micro":0}',$4,'pending') ON CONFLICT(id) DO NOTHING`, r.OperationID, a.UserID, fingerprint[:], body)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			var identical bool
			if e = tx.QueryRow(ctx, `SELECT fingerprint=$2 FROM v3_channelmarket.income_reclaims WHERE id=$1`, r.OperationID, fingerprint[:]).Scan(&identical); e != nil {
				return e
			}
			if !identical {
				return ErrConflict
			}
			if _, e = tx.Exec(ctx, `UPDATE v3_channelmarket.income_reclaims SET status='pending',error_message='',updated_at=$2 WHERE id=$1 AND status='failed'`, r.OperationID, s.cfg.Now()); e != nil {
				return e
			}
		}
		return scanReclaim(tx.QueryRow(ctx, `SELECT id,status,count,amount_micro,batch_number,error_message,created_at,updated_at FROM v3_channelmarket.income_reclaims WHERE id=$1`, r.OperationID), &result)
	})
	return result, err
}
func scanReclaim(row scanner, result *ReclaimJob) error {
	return row.Scan(&result.ID, &result.Status, &result.Count, &result.Amount, &result.Batch, &result.Error, &result.CreatedAt, &result.UpdatedAt)
}
func (s *Service) GetReclaim(ctx context.Context, a Actor, id string) (ReclaimJob, error) {
	var result ReclaimJob
	if !a.Admin {
		return result, ErrNotFound
	}
	if s.pool == nil {
		return result, ErrUnavailable
	}
	err := scanReclaim(s.pool.QueryRow(ctx, `SELECT id,status,count,amount_micro,batch_number,error_message,created_at,updated_at FROM v3_channelmarket.income_reclaims WHERE id=$1`, id), &result)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return result, err
}
