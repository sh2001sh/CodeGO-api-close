package marketplace

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
)

// openItemRow is a single available inventory item locked for opening.
type openItemRow struct {
	id, purchase, pool int64
	rewards            []byte
	guarantees         []byte
	frozen             []byte
	currentPool        bool
	guarantee          string
	scope              string
	paid               bool
}

func (s *Service) OpenBoxes(ctx context.Context, userID int64, requestID string, count int) ([]OpenRecord, error) {
	return s.openBoxes(ctx, userID, requestID, count, 0, nil, count)
}

// OpenBoxesFromPool selects the user's available inventory from one pool.
// Omitting the pool retains the original integer operation fingerprint so old
// requests can still be replayed after the inventory chooser is introduced.
func (s *Service) OpenBoxesFromPool(ctx context.Context, userID int64, requestID string, count int, poolID int64) ([]OpenRecord, error) {
	if poolID < 0 {
		return nil, ErrInvalidInput
	}
	if poolID == 0 {
		return s.OpenBoxes(ctx, userID, requestID, count)
	}
	input := struct {
		Count  int
		PoolID int64
	}{count, poolID}
	return s.openBoxes(ctx, userID, requestID, count, poolID, nil, input)
}

// OpenBoxesFromInventory distinguishes stored and dynamic inventory in the
// same historical pool without changing older operation fingerprints.
func (s *Service) OpenBoxesFromInventory(ctx context.Context, userID int64, requestID string, count int, poolID int64, drawCurrent *bool) ([]OpenRecord, error) {
	if drawCurrent == nil {
		return s.OpenBoxesFromPool(ctx, userID, requestID, count, poolID)
	}
	if poolID <= 0 {
		return nil, ErrInvalidInput
	}
	input := struct {
		Count       int
		PoolID      int64
		DrawCurrent bool
	}{count, poolID, *drawCurrent}
	return s.openBoxes(ctx, userID, requestID, count, poolID, drawCurrent, input)
}

func (s *Service) openBoxes(ctx context.Context, userID int64, requestID string, count int, poolID int64, drawCurrent *bool, input any) ([]OpenRecord, error) {
	var records []OpenRecord
	if err := validRequest(userID, requestID, count); err != nil {
		return nil, err
	}
	account, err := s.wallets.WalletAccount(ctx, userID)
	if err != nil {
		return nil, err
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockUser(ctx, tx, userID); err != nil {
			return err
		}
		found, err := replay(ctx, tx, userID, "open", requestID, input, &records)
		if err != nil || found {
			return err
		}
		if err := s.EnsureExternalInventoryTx(ctx, tx, userID); err != nil {
			return err
		}
		items, err := s.fetchOpenableItemsTx(ctx, tx, userID, count, poolID, drawCurrent)
		if err != nil {
			return err
		}
		poolCounts := make(map[int64]int)
		for _, i := range items {
			poolCounts[i.pool]++
		}
		if err := s.checkOpenLimitsTx(ctx, tx, userID, poolCounts); err != nil {
			return err
		}
		states := make(map[int64]*PityState)
		for index, i := range items {
			record, err := s.openSingleItemTx(ctx, tx, account, userID, requestID, index, i, states)
			if err != nil {
				return err
			}
			records = append(records, record)
		}
		for poolID, state := range states {
			if _, err := tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_pity SET opened=$3,small_progress=$4,big_progress=$5,updated_at=$6 WHERE user_id=$1 AND pool_id=$2`, userID, poolID, state.Opened, state.SmallProgress, state.BigProgress, s.cfg.Now()); err != nil {
				return err
			}
		}
		return remember(ctx, tx, userID, "open", requestID, input, records)
	})
	return records, err
}

// fetchOpenableItemsTx locks and loads exactly `count` available inventory
// items for userID, earliest expiry first. Returns ErrInventory if fewer are found.
func (s *Service) fetchOpenableItemsTx(ctx context.Context, tx pgx.Tx, userID int64, count int, poolID int64, drawCurrent *bool) ([]openItemRow, error) {
	rows, err := tx.Query(ctx, `SELECT i.id,i.purchase_id,i.pool_id,i.rewards,i.guarantees,i.frozen_reward,i.draw_current_pool,i.guarantee_type,b.scope,(NOT p.is_grant AND (p.external_order_id IS NULL OR o.source='purchase' AND o.amount_minor>0))`+
		openableInventoryFrom+openableInventoryWhere+`
 AND ($4::bigint=0 OR i.pool_id=$4)
 AND ($5::boolean IS NULL OR i.draw_current_pool=$5)
 ORDER BY LEAST(i.expires_at,o.expires_at) NULLS LAST,i.id LIMIT $3 FOR UPDATE OF i`, userID, s.cfg.Now(), count, poolID, drawCurrent)
	if err != nil {
		return nil, err
	}
	items := make([]openItemRow, 0, count)
	for rows.Next() {
		var i openItemRow
		if err := rows.Scan(&i.id, &i.purchase, &i.pool, &i.rewards, &i.guarantees, &i.frozen, &i.currentPool, &i.guarantee, &i.scope, &i.paid); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, i)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(items) != count {
		return nil, ErrInventory
	}
	return items, nil
}

// openSingleItemTx resolves the reward for one locked item, persists the
// open record, grants the reward, and marks the item opened.
func (s *Service) openSingleItemTx(ctx context.Context, tx pgx.Tx, account, userID int64, requestID string, index int, i openItemRow, states map[int64]*PityState) (OpenRecord, error) {
	state, err := s.pityTx(ctx, tx, userID, i.pool, states)
	if err != nil {
		return OpenRecord{}, err
	}
	reward, guarantee, err := s.resolveItemRewardTx(ctx, tx, userID, i, state)
	if err != nil {
		return OpenRecord{}, err
	}
	return s.persistOpenedItemTx(ctx, tx, account, userID, requestID, index, i, reward, guarantee)
}

// resolveItemRewardTx determines the reward and guarantee tier for a locked
// item, applying zero-hour, standard-policy, frozen-reward and pity-draw
// rules in the same precedence as the original inline logic.
func (s *Service) resolveItemRewardTx(ctx context.Context, tx pgx.Tx, userID int64, i openItemRow, state *PityState) (Reward, string, error) {
	rewards, guarantees, drawPool, err := s.loadItemRewardSourceTx(ctx, tx, i)
	if err != nil {
		return Reward{}, "", err
	}
	zeroHit, zeroReward, err := s.resolveOpeningZeroHourTx(ctx, tx, userID, i, guarantees, drawPool, state)
	if err != nil {
		return Reward{}, "", err
	}
	var reward Reward
	guarantee := i.guarantee
	if i.frozen == nil && drawPool.Standard.Enabled {
		reward, guarantee, err = s.standardDrawTx(ctx, tx, userID, i.purchase, drawPool, state)
	} else if zeroHit {
		reward = zeroReward
		state.Opened++
		state.SmallProgress = 0
		state.BigProgress = 0
		guarantee = "none"
	} else if i.frozen != nil {
		if err := json.Unmarshal(i.frozen, &reward); err != nil {
			return Reward{}, "", err
		}
		reward.Weight = 1
		advancePity(reward, guarantees, state)
	} else {
		reward, guarantee, err = drawWithGuarantee(rewards, guarantees, state, s.cfg.Draw)
	}
	if err != nil {
		return Reward{}, "", err
	}
	return reward, guarantee, nil
}

// loadItemRewardSourceTx decodes the item's stored rewards/guarantees, and
// when the item draws from the pool's current configuration, loads and
// substitutes the live pool rewards/guarantees instead.
func (s *Service) loadItemRewardSourceTx(ctx context.Context, tx pgx.Tx, i openItemRow) ([]Reward, Guarantees, Pool, error) {
	var drawPool Pool
	var rewards []Reward
	if err := json.Unmarshal(i.rewards, &rewards); err != nil {
		return nil, Guarantees{}, drawPool, err
	}
	var guarantees Guarantees
	if err := json.Unmarshal(i.guarantees, &guarantees); err != nil {
		return nil, Guarantees{}, drawPool, err
	}
	if i.currentPool && i.frozen == nil {
		p, err := loadPool(ctx, tx, i.pool)
		if err != nil {
			return nil, Guarantees{}, drawPool, err
		}
		if !p.Enabled {
			return nil, Guarantees{}, drawPool, ErrUnavailable
		}
		rewards = p.Rewards
		guarantees = p.Guarantees
		drawPool = p
	}
	return rewards, guarantees, drawPool, nil
}

// resolveOpeningZeroHourTx runs the legacy (non-standard-policy) zero-hour
// hidden-reward check, only when neither a first-pity nor due-pity draw
// applies, and otherwise unconditionally advances the paid zero-hour counter.
func (s *Service) resolveOpeningZeroHourTx(ctx context.Context, tx pgx.Tx, userID int64, i openItemRow, guarantees Guarantees, drawPool Pool, state *PityState) (bool, Reward, error) {
	if i.scope != "standard" || i.frozen != nil || drawPool.Standard.Enabled {
		return false, Reward{}, nil
	}
	first := state.Opened == 0 && len(guarantees.First) > 0
	due := guarantees.BigAfter > 0 && state.BigProgress >= guarantees.BigAfter-1 || guarantees.SmallAfter > 0 && state.SmallProgress >= guarantees.SmallAfter-1
	zeroHit := false
	var reward Reward
	if !first && !due {
		var err error
		reward, zeroHit, err = s.tryZeroHourTx(ctx, tx, userID, i.paid)
		if err != nil {
			return false, Reward{}, err
		}
	}
	if !zeroHit {
		if err := s.advancePaidZeroHourTx(ctx, tx, userID, i.paid); err != nil {
			return false, Reward{}, err
		}
	}
	return zeroHit, reward, nil
}

// persistOpenedItemTx inserts the open record, grants the reward, and marks
// the source item opened, in the same order as the original inline logic.
func (s *Service) persistOpenedItemTx(ctx context.Context, tx pgx.Tx, account, userID int64, requestID string, index int, i openItemRow, reward Reward, guarantee string) (OpenRecord, error) {
	record := OpenRecord{ItemID: i.id, Reward: reward, CreatedAt: s.cfg.Now(), Guarantee: guarantee}
	payload, err := json.Marshal(reward)
	if err != nil {
		return OpenRecord{}, err
	}
	key := fmt.Sprintf("%s:%d", requestID, index)
	if err := tx.QueryRow(ctx, `INSERT INTO v3_marketplace.blind_box_open_records(item_id,user_id,request_id,reward,created_at,guarantee_type,pool_id) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`, i.id, userID, key, payload, record.CreatedAt, guarantee, i.pool).Scan(&record.ID); err != nil {
		return OpenRecord{}, err
	}
	if err := s.RecordExternalOpenTx(ctx, tx, i.purchase, record.ID); err != nil {
		return OpenRecord{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_open_records SET pool_type=$2,is_pity=$3,reward_tier=$4,reward_wallet_type=$5 WHERE id=$1`, record.ID, i.scope, guarantee == "small" || guarantee == "big", reward.RewardTier, reward.WalletType); err != nil {
		return OpenRecord{}, err
	}
	if err := s.grantOpenRewardTx(ctx, tx, account, userID, requestID, index, i, reward, &record); err != nil {
		return OpenRecord{}, err
	}
	tag, err := tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_items SET status='opened',opened_at=$3,updated_at=$3,open_record_id=$4 WHERE id=$1 AND owner_user_id=$2 AND status='available'`, i.id, userID, record.CreatedAt, record.ID)
	if err != nil {
		return OpenRecord{}, err
	}
	if tag.RowsAffected() != 1 {
		return OpenRecord{}, ErrConflict
	}
	return record, nil
}

// grantOpenRewardTx credits or records the reward according to its kind,
// mirroring the original switch over reward.Kind exactly.
func (s *Service) grantOpenRewardTx(ctx context.Context, tx pgx.Tx, account, userID int64, requestID string, index int, i openItemRow, reward Reward, record *OpenRecord) error {
	var err error
	switch reward.Kind {
	case "credits":
		_, err = s.money.PostTx(ctx, tx, billing.Entry{AccountID: account, Amount: reward.Amount, Kind: "reward", OperationID: operation("open", userID, requestID, index), Reason: "blind_box_reward"})
	case "subscription":
		if i.scope == "standard" {
			if s.subscriptions == nil {
				return ErrUnavailable
			}
			err = s.subscriptions.GrantRewardTx(ctx, tx, userID, reward.PlanID, operation("open", userID, requestID, index))
			break
		}
		fallthrough
	case "multiplier", "topup_discount", "subscription_discount":
		err = tx.QueryRow(ctx, `INSERT INTO v3_marketplace.blind_box_props(user_id,open_record_id,kind,title,multiplier_ppm,duration_seconds,remaining_seconds,plan_id,prop_type,discount_rate_ppm,max_discount_micro) VALUES($1,$2,$3,$4,$5,$6,$6,NULLIF($7,0),$8,$9,$10) RETURNING id`, userID, record.ID, reward.Kind, reward.Title, reward.MultiplierPPM, reward.DurationSeconds, reward.PlanID, reward.PropType, reward.DiscountRatePPM, reward.MaxDiscountMicro).Scan(&record.PropID)
	case "extra_draw":
		_, err = tx.Exec(ctx, `INSERT INTO v3_marketplace.blind_box_items(purchase_id,owner_user_id,purchase_user_id,pool_id,rewards,guarantees,draw_current_pool) VALUES($1,$2,$2,$3,$4,$5,$6)`, i.purchase, userID, i.pool, i.rewards, i.guarantees, i.currentPool)
	default:
		return ErrInvalidInput
	}
	return err
}
