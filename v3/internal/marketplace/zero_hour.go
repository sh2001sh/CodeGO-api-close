package marketplace

import (
	"context"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

const (
	zeroHourPointCap  = int64(1000)
	zeroHourDrawLimit = int64(10000000)
	zeroHourPropType  = "zero_hour_multiplier"
)

type zeroHourState struct {
	Points, UsageMicro int64
}

func zeroHourDrawThreshold(points int64) int64 {
	return 1000 + min(max(points, 0), zeroHourPointCap)*49
}

func (s *Service) zeroHourStateTx(ctx context.Context, tx pgx.Tx, userID int64) (zeroHourState, error) {
	var state zeroHourState
	if userID <= 0 {
		return state, ErrInvalidInput
	}
	if _, err := tx.Exec(ctx, `INSERT INTO v3_marketplace.blind_box_zero_hour_states(user_id,updated_at) VALUES($1,$2) ON CONFLICT(user_id) DO NOTHING`, userID, s.cfg.Now()); err != nil {
		return state, err
	}
	err := tx.QueryRow(ctx, `SELECT points,usage_micro FROM v3_marketplace.blind_box_zero_hour_states WHERE user_id=$1 FOR UPDATE`, userID).Scan(&state.Points, &state.UsageMicro)
	return state, err
}

// tryZeroHourTx runs before the ordinary standard-pool draw, after the caller
// excludes first-purchase and guaranteed draws. Free orders can also win;
// paid affects only advancePaidZeroHourTx, which must not run after a hit.
func (s *Service) tryZeroHourTx(ctx context.Context, tx pgx.Tx, userID int64, _ bool) (Reward, bool, error) {
	state, err := s.zeroHourStateTx(ctx, tx, userID)
	if err != nil {
		return Reward{}, false, err
	}
	var blocked bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_marketplace.blind_box_props WHERE user_id=$1 AND prop_type=$2 AND (status IN('available','paused') OR (status='active' AND expires_at>$3)))`, userID, zeroHourPropType, s.cfg.Now()).Scan(&blocked)
	if err != nil || blocked {
		return Reward{}, false, err
	}
	draw, err := s.cfg.Draw(zeroHourDrawLimit)
	if err != nil {
		return Reward{}, false, err
	}
	if draw < 0 || draw >= zeroHourDrawLimit {
		return Reward{}, false, ErrInvalidInput
	}
	if draw >= zeroHourDrawThreshold(state.Points) {
		return Reward{}, false, nil
	}
	if _, err := tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_zero_hour_states SET points=0,usage_micro=0,hit_count=hit_count+1,updated_at=$2 WHERE user_id=$1`, userID, s.cfg.Now()); err != nil {
		return Reward{}, false, err
	}
	return Reward{Kind: "multiplier", Title: "1 小时 0 倍率卡", Weight: 1, MultiplierPPM: 0, DurationSeconds: int64(time.Hour / time.Second), PropType: zeroHourPropType}, true, nil
}

// advancePaidZeroHourTx advances a non-winning paid standard open before the
// ordinary draw; the whole open transaction rolls it back on any failure.
func (s *Service) advancePaidZeroHourTx(ctx context.Context, tx pgx.Tx, userID int64, paid bool) error {
	if !paid {
		return nil
	}
	if _, err := s.zeroHourStateTx(ctx, tx, userID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_zero_hour_states SET points=LEAST(points+5,1000),updated_at=$2 WHERE user_id=$1`, userID, s.cfg.Now())
	return err
}

// RecordUsageTx is the asynchronous ledger worker hook. Call it before taking
// ledger account locks, matching the open transaction's state-to-account order.
// The request fingerprint and progress commit with the settled money entry.
func (s *Service) RecordUsageTx(ctx context.Context, tx pgx.Tx, userID int64, requestID string, amount credits.Micro) error {
	if err := validRequest(userID, requestID, 1); err != nil {
		return err
	}
	if amount < 0 {
		return ErrInvalidInput
	}
	state, err := s.zeroHourStateTx(ctx, tx, userID)
	if err != nil {
		return err
	}
	var response struct{}
	found, err := replay(ctx, tx, userID, "zero_hour_usage", requestID, amount, &response)
	if err != nil || found {
		return err
	}
	if int64(amount) > math.MaxInt64-state.UsageMicro {
		return credits.ErrOverflow
	}
	usage := state.UsageMicro + int64(amount)
	points := min(zeroHourPointCap, state.Points+usage/1000000-state.UsageMicro/1000000)
	if _, err := tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_zero_hour_states SET points=$2,usage_micro=$3,updated_at=$4 WHERE user_id=$1`, userID, points, usage, s.cfg.Now()); err != nil {
		return err
	}
	return remember(ctx, tx, userID, "zero_hour_usage", requestID, amount, response)
}

type ZeroHourOverview struct {
	CurrentProbability float64 `json:"current_probability"`
	MaxProbability     float64 `json:"max_probability"`
	Points             int64   `json:"points"`
	PointCap           int64   `json:"point_cap"`
	Active             bool    `json:"active"`
	ActiveUntil        int64   `json:"active_until"`
}

func (s *Service) ZeroHourOverview(ctx context.Context, userID int64) (ZeroHourOverview, error) {
	result := ZeroHourOverview{CurrentProbability: .0001, MaxProbability: .005, PointCap: zeroHourPointCap}
	if userID <= 0 {
		return result, ErrInvalidInput
	}
	var activeUntil *time.Time
	err := s.pool.QueryRow(ctx, `SELECT COALESCE((SELECT points FROM v3_marketplace.blind_box_zero_hour_states WHERE user_id=$1),0), (SELECT max(expires_at) FROM v3_marketplace.blind_box_props WHERE user_id=$1 AND prop_type=$2 AND status='active' AND expires_at>$3)`, userID, zeroHourPropType, s.cfg.Now()).Scan(&result.Points, &activeUntil)
	if err != nil {
		return result, err
	}
	result.CurrentProbability = float64(zeroHourDrawThreshold(result.Points)) / float64(zeroHourDrawLimit)
	if activeUntil != nil {
		result.Active = true
		result.ActiveUntil = activeUntil.Unix()
	}
	return result, nil
}
