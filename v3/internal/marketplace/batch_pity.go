package marketplace

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

const paidRandomDailyLimit = 10

type BatchPityPolicy struct {
	SmallAfter   int           `json:"small_after"`
	SmallMinimum credits.Micro `json:"small_minimum_micro"`
	BigAfter     int           `json:"big_after"`
	BigMinimum   credits.Micro `json:"big_minimum_micro"`
}

func batchPityPolicy(price credits.Micro) BatchPityPolicy {
	return BatchPityPolicy{SmallAfter: 10, SmallMinimum: price, BigAfter: 50, BigMinimum: price * 2}
}

// drawBatchRecord changes only the copied base pool and progress. Both paid
// settlement and simulations use this function; supplements never replace a prize.
func drawBatchRecord(b *Batch, state *PityState, draw func(int64) (int64, error)) (OpenRecord, error) {
	var out OpenRecord
	if b.RemainingCount <= 0 {
		return out, ErrInventory
	}
	n, err := draw(b.RemainingCount)
	if err != nil {
		return out, err
	}
	if n < 0 || n >= b.RemainingCount {
		return out, ErrInvalidInput
	}
	for i := range b.Rewards {
		r := &b.Rewards[i]
		if n >= r.Remaining {
			n -= r.Remaining
			continue
		}
		out = OpenRecord{Guarantee: "none", BatchID: b.ID, Reward: Reward{Kind: r.Kind, Title: r.Title, Amount: r.Amount, PlanID: r.PlanID, Weight: 1, WalletType: "api_only", PlanSnapshot: r.PlanSnapshot}}
		if b.Purpose == "paid_random" {
			out.GuaranteeCredits, out.Guarantee, err = advanceBatchPity(b.PityPolicy, state, *r)
			if err != nil {
				return OpenRecord{}, err
			}
		}
		r.Remaining--
		b.RemainingCount--
		return out, nil
	}
	return out, ErrConflict
}

func advanceBatchPity(p BatchPityPolicy, state *PityState, reward BatchReward) (credits.Micro, string, error) {
	if p.SmallAfter != 10 || p.BigAfter != 50 || p.SmallMinimum <= 0 || p.SmallMinimum > credits.Micro(math.MaxInt64/2) || p.BigMinimum != p.SmallMinimum*2 || state.Opened < 0 || state.Opened == math.MaxInt64 || state.SmallProgress < 0 || state.SmallProgress >= p.SmallAfter || state.BigProgress < 0 || state.BigProgress >= p.BigAfter {
		return 0, "none", ErrConflict
	}
	value := credits.Micro(0)
	if reward.Kind == "credits" {
		value = reward.Amount
	}
	minimum, kind := credits.Micro(0), "none"
	if state.BigProgress+1 >= p.BigAfter && value < p.BigMinimum {
		minimum, kind = p.BigMinimum, "big"
	} else if state.SmallProgress+1 >= p.SmallAfter && value < p.SmallMinimum {
		minimum, kind = p.SmallMinimum, "small"
	}
	supplement := credits.Micro(0)
	if minimum > value {
		supplement, value = minimum-value, minimum
	}
	state.Opened++
	if value >= p.BigMinimum {
		state.SmallProgress, state.BigProgress = 0, 0
	} else {
		state.BigProgress++
		if value >= p.SmallMinimum {
			state.SmallProgress = 0
		} else {
			state.SmallProgress++
		}
	}
	return supplement, kind, nil
}

func readBatchPityTx(ctx context.Context, tx pgx.Tx, user int64) (PityState, error) {
	var state PityState
	err := tx.QueryRow(ctx, `SELECT opened,small_progress,big_progress FROM v3_marketplace.blind_box_batch_pity WHERE user_id=$1`, user).Scan(&state.Opened, &state.SmallProgress, &state.BigProgress)
	if errors.Is(err, pgx.ErrNoRows) {
		return state, nil
	}
	return state, err
}

func lockBatchPityTx(ctx context.Context, tx pgx.Tx, user int64) (PityState, error) {
	var state PityState
	if _, err := tx.Exec(ctx, `INSERT INTO v3_marketplace.blind_box_batch_pity(user_id) VALUES($1) ON CONFLICT(user_id) DO NOTHING`, user); err != nil {
		return state, err
	}
	err := tx.QueryRow(ctx, `SELECT opened,small_progress,big_progress FROM v3_marketplace.blind_box_batch_pity WHERE user_id=$1 FOR UPDATE`, user).Scan(&state.Opened, &state.SmallProgress, &state.BigProgress)
	return state, err
}

func (s *Service) batchDailyPurchasedTx(ctx context.Context, tx pgx.Tx, user int64) (int64, error) {
	now := s.cfg.Now().In(time.FixedZone("Asia/Shanghai", 8*60*60))
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	var count int64
	err := tx.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM v3_marketplace.blind_box_open_records r JOIN v3_marketplace.blind_box_batches b ON b.id=r.batch_id WHERE r.user_id=$1 AND b.price_micro>0 AND r.created_at>=$2 AND r.created_at<$3)
+(SELECT coalesce(sum(quantity),0) FROM v3_marketplace.blind_box_orders WHERE user_id=$1 AND source='purchase' AND status IN('pending','success','completed') AND created_at>=$2 AND created_at<$3)
+(SELECT coalesce(sum(quantity),0) FROM v3_marketplace.blind_box_purchases WHERE user_id=$1 AND NOT is_grant AND external_order_id IS NULL AND status='completed' AND purchase_date=$4::date)`, user, day, day.AddDate(0, 0, 1), day.Format("2006-01-02")).Scan(&count)
	return count, err
}
