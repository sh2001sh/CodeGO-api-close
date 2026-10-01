package marketplace

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

type Overview struct {
	AvailableCount int64               `json:"available_count"`
	Pools          []Pool              `json:"pools"`
	Props          []Prop              `json:"props"`
	Pity           PityState           `json:"pity"`
	PityStates     map[int64]PityState `json:"pity_states"`
	ZeroHour       ZeroHourOverview    `json:"zero_hour"`
}

func (s *Service) Overview(ctx context.Context, userID int64) (Overview, error) {
	var result Overview
	if userID <= 0 {
		return result, ErrInvalidInput
	}
	if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockUser(ctx, tx, userID); err != nil {
			return err
		}
		return s.EnsureExternalInventoryTx(ctx, tx, userID)
	}); err != nil {
		return result, err
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM v3_marketplace.blind_box_items i JOIN v3_marketplace.blind_box_purchases p ON p.id=i.purchase_id LEFT JOIN v3_marketplace.blind_box_orders o ON o.id=p.external_order_id WHERE i.owner_user_id=$1 AND i.status='available' AND p.status='completed' AND (i.expires_at IS NULL OR i.expires_at>$2) AND (p.external_order_id IS NULL OR o.status IN('success','completed') AND (o.expires_at IS NULL OR o.expires_at>$2))`, userID, s.cfg.Now()).Scan(&result.AvailableCount); err != nil {
		return result, err
	}
	var err error
	result.ZeroHour, err = s.ZeroHourOverview(ctx, userID)
	if err != nil {
		return result, err
	}
	result.Pity, result.PityStates, err = s.loadPityStates(ctx, userID)
	if err != nil {
		return result, err
	}
	result.Pools, err = s.loadEnabledPools(ctx)
	if err != nil {
		return result, err
	}
	result.Props, err = s.loadUserProps(ctx, userID)
	return result, err
}

// loadPityStates returns every pity row for userID keyed by pool id, along
// with the first row encountered (matching the original "first wins" default
// for the top-level Pity field).
func (s *Service) loadPityStates(ctx context.Context, userID int64) (PityState, map[int64]PityState, error) {
	states := make(map[int64]PityState)
	var first PityState
	rows, err := s.pool.Query(ctx, `SELECT pool_id,opened,small_progress,big_progress FROM v3_marketplace.blind_box_pity WHERE user_id=$1 ORDER BY pool_id`, userID)
	if err != nil {
		return first, states, err
	}
	for rows.Next() {
		var id int64
		var state PityState
		if err := rows.Scan(&id, &state.Opened, &state.SmallProgress, &state.BigProgress); err != nil {
			rows.Close()
			return first, states, err
		}
		if len(states) == 0 {
			first = state
		}
		states[id] = state
	}
	err = rows.Err()
	rows.Close()
	return first, states, err
}

// loadEnabledPools loads every enabled blind-box pool with its rewards,
// guarantees and standard policy decoded.
func (s *Service) loadEnabledPools(ctx context.Context) ([]Pool, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,name,enabled,price_micro,daily_limit,rewards,guarantees,scope,monthly_limit,daily_open_limit,standard_policy FROM v3_marketplace.blind_box_pools WHERE enabled ORDER BY id`)
	if err != nil {
		return nil, err
	}
	pools := make([]Pool, 0)
	for rows.Next() {
		var p Pool
		var rewards, guarantees, standard []byte
		if err := rows.Scan(&p.ID, &p.Name, &p.Enabled, &p.Price, &p.DailyLimit, &rewards, &guarantees, &p.Scope, &p.MonthlyLimit, &p.DailyOpenLimit, &standard); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal(rewards, &p.Rewards); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal(guarantees, &p.Guarantees); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal(standard, &p.Standard); err != nil {
			rows.Close()
			return nil, err
		}
		pools = append(pools, p)
	}
	err = rows.Err()
	rows.Close()
	return pools, err
}

// loadUserProps loads every blind-box prop owned by userID, newest first.
func (s *Service) loadUserProps(ctx context.Context, userID int64) ([]Prop, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,kind,title,CASE WHEN status='active' AND expires_at<=$2 THEN 'expired' ELSE status END,coalesce(multiplier_ppm,1000000),remaining_seconds,plan_id,expires_at,prop_type,discount_rate_ppm,max_discount_micro,used_discount_micro FROM v3_marketplace.blind_box_props WHERE user_id=$1 ORDER BY id DESC`, userID, s.cfg.Now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	props := make([]Prop, 0)
	for rows.Next() {
		var p Prop
		if err := rows.Scan(&p.ID, &p.Kind, &p.Title, &p.Status, &p.MultiplierPPM, &p.RemainingSeconds, &p.PlanID, &p.ExpiresAt, &p.PropType, &p.DiscountRatePPM, &p.MaxDiscountMicro, &p.UsedDiscountMicro); err != nil {
			return nil, err
		}
		props = append(props, p)
	}
	return props, rows.Err()
}

func (s *Service) History(ctx context.Context, userID, before int64, limit int) ([]OpenRecord, error) {
	if userID <= 0 {
		return nil, ErrInvalidInput
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	if before <= 0 {
		before = 9223372036854775807
	}
	rows, err := s.pool.Query(ctx, `SELECT r.id,coalesce(r.item_id,0),r.reward,coalesce(p.id,0),r.created_at,r.guarantee_type FROM v3_marketplace.blind_box_open_records r LEFT JOIN v3_marketplace.blind_box_props p ON p.open_record_id=r.id WHERE r.user_id=$1 AND r.id<$2 ORDER BY r.id DESC LIMIT $3`, userID, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]OpenRecord, 0)
	for rows.Next() {
		var r OpenRecord
		var payload []byte
		if err := rows.Scan(&r.ID, &r.ItemID, &payload, &r.PropID, &r.CreatedAt, &r.Guarantee); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(payload, &r.Reward); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, rows.Err()
}
