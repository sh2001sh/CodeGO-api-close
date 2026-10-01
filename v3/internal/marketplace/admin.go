package marketplace

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

func adminTx(ctx context.Context, tx pgx.Tx, id int64) error {
	var role string
	err := tx.QueryRow(ctx, `SELECT role FROM v3_identity.users WHERE id=$1 AND status='active' AND deleted_at IS NULL`, id).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && role != "admin" && role != "root") {
		return ErrUnavailable
	}
	return err
}

func (s *Service) GrantBoxes(ctx context.Context, adminID, userID int64, requestID string, poolID int64, count int) (Purchase, error) {
	return s.GrantBoxesWithReason(ctx, adminID, userID, requestID, poolID, count, "")
}

func (s *Service) GrantBoxesWithReason(ctx context.Context, adminID, userID int64, requestID string, poolID int64, count int, reason string) (Purchase, error) {
	var purchase Purchase
	if err := validRequest(userID, requestID, count); err != nil {
		return purchase, err
	}
	if poolID <= 0 || adminID <= 0 || len(reason) > 2000 {
		return purchase, ErrInvalidInput
	}
	input := struct {
		Admin, PoolID int64
		Count         int
		Reason        string
	}{adminID, poolID, count, reason}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := adminTx(ctx, tx, adminID); err != nil {
			return err
		}
		if err := lockUser(ctx, tx, userID); err != nil {
			return err
		}
		found, err := replay(ctx, tx, userID, "grant", requestID, input, &purchase)
		if err != nil || found {
			return err
		}
		p, err := loadPool(ctx, tx, poolID)
		if err != nil {
			return err
		}
		purchase.Quantity = count
		if err := tx.QueryRow(ctx, `INSERT INTO v3_marketplace.blind_box_purchases(user_id,pool_id,quantity,unit_price_micro,purchase_date,request_id,is_grant) VALUES($1,$2,$3,0,$4,$5,true) RETURNING id`, userID, poolID, count, s.cfg.Now().In(s.cfg.Location).Format("2006-01-02"), "grant:"+requestID).Scan(&purchase.ID); err != nil {
			return err
		}
		rewards, err := json.Marshal(p.Rewards)
		if err != nil {
			return err
		}
		guarantees, err := json.Marshal(p.Guarantees)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO v3_marketplace.blind_box_items(purchase_id,owner_user_id,purchase_user_id,pool_id,rewards,guarantees) SELECT $1,$2,$2,$3,$4,$5 FROM generate_series(1,$6::int)`, purchase.ID, userID, poolID, rewards, guarantees, count); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO v3_marketplace.blind_box_grants(user_id,admin_user_id,quantity,reason,idempotency_key,trade_no,created_at) VALUES($1,$2,$3,$4,$5,$5,$6)`, userID, adminID, count, reason, operation("grant", userID, requestID, 0), s.cfg.Now()); err != nil {
			return err
		}
		return remember(ctx, tx, userID, "grant", requestID, input, purchase)
	})
	return purchase, err
}

func (s *Service) RevokeBoxes(ctx context.Context, adminID, userID int64, requestID string, count int) ([]int64, error) {
	return s.RevokeBoxesWithReason(ctx, adminID, userID, requestID, count, "")
}

func (s *Service) RevokeBoxesWithReason(ctx context.Context, adminID, userID int64, requestID string, count int, reason string) ([]int64, error) {
	var ids []int64
	if err := validRequest(userID, requestID, count); err != nil {
		return nil, err
	}
	if adminID <= 0 || len(reason) > 2000 {
		return nil, ErrInvalidInput
	}
	input := struct {
		Admin  int64
		Count  int
		Reason string
	}{adminID, count, reason}
	var receipt struct {
		IDs    []int64 `json:"item_ids"`
		Reason string  `json:"reason"`
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := adminTx(ctx, tx, adminID); err != nil {
			return err
		}
		if err := lockUser(ctx, tx, userID); err != nil {
			return err
		}
		found, err := replay(ctx, tx, userID, "revoke", requestID, input, &receipt)
		if err != nil || found {
			ids = receipt.IDs
			return err
		}
		ids, err = revokeAvailableItemsTx(ctx, tx, userID, count, s.cfg.Now())
		if err != nil {
			return err
		}
		receipt.IDs, receipt.Reason = ids, reason
		return remember(ctx, tx, userID, "revoke", requestID, input, receipt)
	})
	return ids, err
}

// revokeAvailableItemsTx locks up to count available inventory items owned
// by userID and marks them revoked, returning ErrInventory if fewer are found.
func revokeAvailableItemsTx(ctx context.Context, tx pgx.Tx, userID int64, count int, now time.Time) ([]int64, error) {
	rows, err := tx.Query(ctx, `SELECT i.id FROM v3_marketplace.blind_box_items i JOIN v3_marketplace.blind_box_purchases p ON p.id=i.purchase_id LEFT JOIN v3_marketplace.blind_box_orders o ON o.id=p.external_order_id WHERE i.owner_user_id=$1 AND i.status='available' AND p.status='completed' AND (i.expires_at IS NULL OR i.expires_at>$3) AND (p.external_order_id IS NULL OR o.status IN('success','completed') AND (o.expires_at IS NULL OR o.expires_at>$3)) ORDER BY i.id LIMIT $2 FOR UPDATE OF i`, userID, count, now)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(ids) != count {
		return nil, ErrInventory
	}
	tag, err := tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_items SET status='revoked' WHERE id=ANY($1::bigint[]) AND owner_user_id=$2 AND status='available'`, ids, userID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() != int64(count) {
		return nil, ErrConflict
	}
	return ids, nil
}

// Simulation is bounded and never changes wallet, inventory, history or pity.
func (s *Service) Simulate(ctx context.Context, poolID int64, count int, state PityState) ([]OpenRecord, PityState, error) {
	if count < 1 || count > 100 || state.Opened < 0 || state.SmallProgress < 0 || state.BigProgress < 0 {
		return nil, state, ErrInvalidInput
	}
	var records []OpenRecord
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		p, err := loadPool(ctx, tx, poolID)
		if err != nil {
			return err
		}
		if !p.Enabled {
			return ErrUnavailable
		}
		if state.SmallProgress > p.Guarantees.SmallAfter || state.BigProgress > p.Guarantees.BigAfter {
			return ErrInvalidInput
		}
		for pending := count; pending > 0; {
			if len(records) >= 10000 {
				return ErrInvalidInput
			}
			pending--
			r, guarantee, err := drawWithGuarantee(p.Rewards, p.Guarantees, &state, s.cfg.Draw)
			if err != nil {
				return err
			}
			records = append(records, OpenRecord{Reward: r, Guarantee: guarantee})
			if r.Kind == "extra_draw" {
				pending++
			}
		}
		return nil
	})
	return records, state, err
}
