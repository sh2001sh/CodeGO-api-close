package commerce

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type RedemptionCode struct {
	ID               int64         `json:"id"`
	Name             string        `json:"name"`
	Credits          credits.Micro `json:"credits"`
	State            string        `json:"state"`
	ExpiresAt        *time.Time    `json:"expires_at"`
	ClaimedBy        *int64        `json:"claimed_by,omitempty"`
	RedeemType       string        `json:"redeem_type"`
	PlanID           int64         `json:"plan_id,omitempty"`
	PlanTitle        string        `json:"plan_title,omitempty"`
	BlindBoxQuantity int           `json:"blind_box_quantity,omitempty"`
	// Key is returned once on issuance; storage and later reads contain only SHA256.
	Key string `json:"key,omitempty"`
}

func (s *Service) IssueRedemption(ctx context.Context, name string, amount credits.Micro, expires *time.Time) (RedemptionCode, error) {
	return s.IssueTypedRedemption(ctx, IssueRedemptionInput{Name: name, Credits: amount, ExpiresAt: expires})
}

func (s *Service) IssueTypedRedemption(ctx context.Context, in IssueRedemptionInput) (RedemptionCode, error) {
	var result RedemptionCode
	if err := s.validateRedemptionInput(ctx, &in); err != nil {
		return result, err
	}
	var random [24]byte
	if _, err := rand.Read(random[:]); err != nil {
		return result, err
	}
	key := "cg_" + hex.EncodeToString(random[:])
	digest := sha256.Sum256([]byte(key))
	result = RedemptionCode{Name: in.Name, Credits: in.Credits, State: "active", ExpiresAt: in.ExpiresAt, Key: key, RedeemType: in.RedeemType, PlanID: in.PlanID, PlanTitle: in.PlanTitle, BlindBoxQuantity: in.BlindBoxQuantity}
	err := s.pool.QueryRow(ctx, `INSERT INTO v3_commerce.redemption_codes(code_hash,name,credits,expires_at,redeem_type,plan_id,plan_title,blind_box_quantity) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
		digest[:], in.Name, int64(in.Credits), in.ExpiresAt, in.RedeemType, in.PlanID, in.PlanTitle, in.BlindBoxQuantity).Scan(&result.ID)
	return result, err
}

func (s *Service) Redeem(ctx context.Context, userID int64, key string) (credits.Micro, error) {
	result, err := s.RedeemTyped(ctx, userID, key)
	return result.Credits, err
}

func (s *Service) RedeemTyped(ctx context.Context, userID int64, key string) (RedemptionResult, error) {
	var result RedemptionResult
	if userID <= 0 || len(key) < 8 || len(key) > 256 || s.poster == nil {
		return result, ErrInvalid
	}
	digest := sha256.Sum256([]byte(key))
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var id int64
		var state string
		var claimed *int64
		var expires *time.Time
		var saved []byte
		err := tx.QueryRow(ctx, `SELECT id,credits,state,claimed_by,expires_at,redeem_type,plan_id,plan_title,blind_box_quantity,redeem_result FROM v3_commerce.redemption_codes WHERE code_hash=$1 AND deleted_at IS NULL FOR UPDATE`, digest[:]).
			Scan(&id, &result.Credits, &state, &claimed, &expires, &result.RedeemType, &result.PlanID, &result.PlanTitle, &result.BlindBoxQuantity, &saved)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if state == "used" && claimed != nil && *claimed == userID {
			return decodeRedemptionResult(saved, &result)
		}
		if state != "active" || (expires != nil && !expires.After(s.cfg.Now())) {
			return ErrStateConflict
		}
		if err = s.applyRedemptionTx(ctx, tx, userID, fmt.Sprintf("redemption:%d", id), &result); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE v3_commerce.redemption_codes SET state='used',claimed_by=$2,claimed_at=$3,redeem_result=$4 WHERE id=$1 AND state='active'`, id, userID, s.cfg.Now(), result)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrStateConflict
		}
		return nil
	})
	if err != nil {
		return RedemptionResult{}, err
	}
	return result, nil
}

func (s *Service) ListRedemptions(ctx context.Context, before int64, limit int) ([]RedemptionCode, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `SELECT id,name,credits,state,expires_at,claimed_by,redeem_type,plan_id,plan_title,blind_box_quantity FROM v3_commerce.redemption_codes
	    WHERE deleted_at IS NULL AND ($1::bigint=0 OR id<$1) ORDER BY id DESC LIMIT $2`, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]RedemptionCode, 0, limit)
	for rows.Next() {
		var item RedemptionCode
		if err = rows.Scan(&item.ID, &item.Name, &item.Credits, &item.State, &item.ExpiresAt, &item.ClaimedBy, &item.RedeemType, &item.PlanID, &item.PlanTitle, &item.BlindBoxQuantity); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Service) RevokeRedemption(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `UPDATE v3_commerce.redemption_codes SET state='revoked' WHERE id=$1 AND state='active' AND deleted_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrStateConflict
	}
	return nil
}
