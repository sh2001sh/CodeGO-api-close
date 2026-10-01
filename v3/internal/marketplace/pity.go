package marketplace

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func (s *Service) pityTx(ctx context.Context, tx pgx.Tx, userID, poolID int64, states map[int64]*PityState) (*PityState, error) {
	if state, ok := states[poolID]; ok {
		return state, nil
	}
	if _, err := tx.Exec(ctx, `INSERT INTO v3_marketplace.blind_box_pity(user_id,pool_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, userID, poolID); err != nil {
		return nil, err
	}
	state := &PityState{}
	if err := tx.QueryRow(ctx, `SELECT opened,small_progress,big_progress FROM v3_marketplace.blind_box_pity WHERE user_id=$1 AND pool_id=$2 FOR UPDATE`, userID, poolID).Scan(&state.Opened, &state.SmallProgress, &state.BigProgress); err != nil {
		return nil, err
	}
	states[poolID] = state
	return state, nil
}
