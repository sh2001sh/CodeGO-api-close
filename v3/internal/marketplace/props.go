package marketplace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
)

type Prop struct {
	ID                int64      `json:"id"`
	Kind              string     `json:"kind"`
	Title             string     `json:"title"`
	Status            string     `json:"status"`
	MultiplierPPM     int64      `json:"multiplier_ppm"`
	RemainingSeconds  int64      `json:"remaining_seconds"`
	PlanID            *int64     `json:"plan_id,omitempty"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	PropType          string     `json:"prop_type"`
	DiscountRatePPM   int64      `json:"discount_rate_ppm"`
	MaxDiscountMicro  int64      `json:"max_discount_micro"`
	UsedDiscountMicro int64      `json:"used_discount_micro"`
}

func legacyUnlimitedProp(propType string) bool {
	switch propType {
	case "consume_discount_95", "consume_discount_90", "consume_discount_10", "zero_hour_multiplier", "monthly_pass_multiplier":
		return true
	default:
		return false
	}
}

func (s *Service) UseProp(ctx context.Context, userID, propID int64) (Prop, error) {
	return s.changeProp(ctx, userID, propID, false)
}
func (s *Service) PauseProp(ctx context.Context, userID, propID int64) (Prop, error) {
	return s.changeProp(ctx, userID, propID, true)
}
func (s *Service) changeProp(ctx context.Context, userID, propID int64, pause bool) (Prop, error) {
	var p Prop
	if userID <= 0 || propID <= 0 {
		return p, ErrInvalidInput
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockUser(ctx, tx, userID); err != nil {
			return err
		}
		if err := s.expirePropsTx(ctx, tx, userID); err != nil {
			return err
		}
		var err error
		p, err = loadLockedPropTx(ctx, tx, userID, propID)
		if err != nil {
			return err
		}
		now := s.cfg.Now()
		var done bool
		if pause {
			done, err = pausePropState(&p, now)
		} else {
			done, err = s.activatePropStateTx(ctx, tx, userID, propID, now, &p)
		}
		if err != nil || done {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_props SET status=$3,remaining_seconds=$4,expires_at=$5,started_at=CASE WHEN $3='active' THEN $6 ELSE started_at END,updated_at=$6 WHERE id=$1 AND user_id=$2`, propID, userID, p.Status, p.RemainingSeconds, p.ExpiresAt, now)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrConflict
		}
		return s.profileTx(ctx, tx, userID)
	})
	return p, err
}

// loadLockedPropTx locks and loads the user's prop row by id.
func loadLockedPropTx(ctx context.Context, tx pgx.Tx, userID, propID int64) (Prop, error) {
	var p Prop
	err := tx.QueryRow(ctx, `SELECT id,kind,title,status,coalesce(multiplier_ppm,1000000),remaining_seconds,plan_id,expires_at,prop_type,discount_rate_ppm,max_discount_micro,used_discount_micro FROM v3_marketplace.blind_box_props WHERE id=$1 AND user_id=$2 FOR UPDATE`, propID, userID).Scan(&p.ID, &p.Kind, &p.Title, &p.Status, &p.MultiplierPPM, &p.RemainingSeconds, &p.PlanID, &p.ExpiresAt, &p.PropType, &p.DiscountRatePPM, &p.MaxDiscountMicro, &p.UsedDiscountMicro)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// pausePropState mutates p in place for the pause branch. The returned bool
// reports whether the caller should stop (no-op success) without persisting.
func pausePropState(p *Prop, now time.Time) (bool, error) {
	if p.Status == "paused" {
		return true, nil
	}
	if p.Kind != "multiplier" || p.Status != "active" || p.ExpiresAt == nil {
		return false, ErrConflict
	}
	p.RemainingSeconds = int64(math.Ceil(p.ExpiresAt.Sub(now).Seconds()))
	if p.RemainingSeconds <= 0 {
		return false, ErrConflict
	}
	p.Status = "paused"
	p.ExpiresAt = nil
	return false, nil
}

// activatePropStateTx mutates p in place for the use/activate branch,
// validating conflicting active props and granting subscription rewards as
// needed. The returned bool reports whether the caller should stop (no-op
// success) without persisting.
func (s *Service) activatePropStateTx(ctx context.Context, tx pgx.Tx, userID, propID int64, now time.Time, p *Prop) (bool, error) {
	if p.Status == "active" || p.Status == "used" {
		return true, nil
	}
	if p.Status != "available" && p.Status != "paused" {
		return false, ErrConflict
	}
	if p.Kind == "topup_discount" || p.Kind == "subscription_discount" {
		return false, ErrConflict
	}
	if p.Kind == "extra_draw" {
		return false, ErrConflict
	}
	if p.Kind == "multiplier" && (p.PropType == "zero_hour_multiplier" || p.PropType == "monthly_pass_multiplier" || p.PropType == "consume_discount_10") {
		var active bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_marketplace.blind_box_props WHERE user_id=$1 AND id<>$2 AND prop_type=$3 AND status='active' AND expires_at>$4)`, userID, propID, p.PropType, now).Scan(&active); err != nil {
			return false, err
		}
		if active {
			return false, ErrConflict
		}
	}
	if p.MaxDiscountMicro > 0 && p.UsedDiscountMicro >= p.MaxDiscountMicro && !legacyUnlimitedProp(p.PropType) {
		return false, ErrConflict
	}
	if p.Kind == "subscription" {
		if s.subscriptions == nil || p.PlanID == nil {
			return false, ErrUnavailable
		}
		if err := s.subscriptions.GrantRewardTx(ctx, tx, userID, *p.PlanID, fmt.Sprintf("blind-box:prop:%d", propID)); err != nil {
			return false, err
		}
		p.Status = "used"
		return false, nil
	}
	if p.RemainingSeconds <= 0 {
		return false, ErrConflict
	}
	p.Status = "active"
	expiry := now.Add(time.Duration(p.RemainingSeconds) * time.Second)
	p.ExpiresAt = &expiry
	return false, nil
}

func (s *Service) expirePropsTx(ctx context.Context, tx pgx.Tx, userID int64) error {
	_, err := tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_props SET status='expired',remaining_seconds=0,updated_at=$2 WHERE user_id=$1 AND status='active' AND expires_at<=$2`, userID, s.cfg.Now())
	return err
}

// Each active multiplier is projected separately. The snapshot can choose the
// next best unexpired card without waiting for a worker at the expiry boundary.
func (s *Service) profileTx(ctx context.Context, tx pgx.Tx, userID int64) error {
	var factor int64 = 1000000
	var expiry *time.Time
	err := tx.QueryRow(ctx, `SELECT multiplier_ppm,expires_at FROM v3_marketplace.blind_box_props WHERE user_id=$1 AND kind='multiplier' AND status='active' AND expires_at>$2 AND (max_discount_micro=0 OR used_discount_micro<max_discount_micro OR prop_type IN('consume_discount_95','consume_discount_90','consume_discount_10','zero_hour_multiplier','monthly_pass_multiplier')) ORDER BY multiplier_ppm,expires_at DESC LIMIT 1`, userID, s.cfg.Now()).Scan(&factor, &expiry)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT id,multiplier_ppm,expires_at,prop_type,max_discount_micro,used_discount_micro FROM v3_marketplace.blind_box_props WHERE user_id=$1 AND kind='multiplier' AND status='active' AND expires_at>$2 AND (max_discount_micro=0 OR used_discount_micro<max_discount_micro OR prop_type IN('consume_discount_95','consume_discount_90','consume_discount_10','zero_hour_multiplier','monthly_pass_multiplier')) ORDER BY multiplier_ppm,expires_at DESC`, userID, s.cfg.Now())
	if err != nil {
		return err
	}
	type card struct {
		MultiplierPPM     int64     `json:"multiplier_ppm"`
		ExpiresAt         time.Time `json:"expires_at"`
		ID                int64     `json:"id"`
		PropType          string    `json:"prop_type"`
		MaxDiscountMicro  int64     `json:"max_discount_micro"`
		UsedDiscountMicro int64     `json:"used_discount_micro"`
	}
	cards := make([]card, 0)
	for rows.Next() {
		var c card
		if err := rows.Scan(&c.ID, &c.MultiplierPPM, &c.ExpiresAt, &c.PropType, &c.MaxDiscountMicro, &c.UsedDiscountMicro); err != nil {
			rows.Close()
			return err
		}
		cards = append(cards, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(cards)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO v3_marketplace.account_profiles(user_id,multiplier_ppm,expires_at,updated_at,cards) VALUES($1,$2,$3,$4,$5) ON CONFLICT(user_id) DO UPDATE SET multiplier_ppm=EXCLUDED.multiplier_ppm,expires_at=EXCLUDED.expires_at,updated_at=EXCLUDED.updated_at,cards=EXCLUDED.cards`, userID, factor, expiry, s.cfg.Now(), payload)
	return err
}

func (s *Service) ExpireProps(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT user_id FROM v3_marketplace.blind_box_props WHERE status='active' AND expires_at<=$1 ORDER BY user_id LIMIT $2`, s.cfg.Now(), limit)
	if err != nil {
		return 0, err
	}
	var users []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		users = append(users, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for _, id := range users {
		err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			var lockedID int64
			if err := tx.QueryRow(ctx, `SELECT id FROM v3_identity.users WHERE id=$1 FOR NO KEY UPDATE`, id).Scan(&lockedID); err != nil {
				return err
			}
			if err := s.expirePropsTx(ctx, tx, id); err != nil {
				return err
			}
			return s.profileTx(ctx, tx, id)
		})
		if err != nil {
			return 0, err
		}
	}
	return len(users), nil
}
