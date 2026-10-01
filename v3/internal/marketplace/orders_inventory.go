package marketplace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// EnsureExternalInventoryTx restores remaining inventory from retained external
// orders. Call after lockUser and before selecting items for open/gift/overview.
// The unique purchase.external_order_id is the durable materialization marker.
func (s *Service) EnsureExternalInventoryTx(ctx context.Context, tx pgx.Tx, userID int64) error {
	if userID <= 0 {
		return ErrInvalidInput
	}
	rows, err := tx.Query(ctx, `SELECT `+boxOrderColumns+` FROM v3_marketplace.blind_box_orders o
 WHERE user_id=$1 AND status IN('success','completed') AND quantity>opened_count
 AND (expires_at IS NULL OR expires_at>$2)
 AND NOT EXISTS(SELECT 1 FROM v3_marketplace.blind_box_purchases p WHERE p.external_order_id=o.id)
 ORDER BY id FOR UPDATE`, userID, s.cfg.Now())
	if err != nil {
		return err
	}
	var orders []BoxOrder
	for rows.Next() {
		o, err := scanBoxOrder(rows)
		if err != nil {
			rows.Close()
			return err
		}
		orders = append(orders, o)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, o := range orders {
		if _, err := s.materializeBoxOrderTx(ctx, tx, o); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) materializeBoxOrderTx(ctx context.Context, tx pgx.Tx, o BoxOrder) (Purchase, error) {
	var result Purchase
	err := tx.QueryRow(ctx, `SELECT id,quantity,unit_price_micro,total_micro FROM v3_marketplace.blind_box_purchases WHERE external_order_id=$1`, o.ID).
		Scan(&result.ID, &result.Quantity, &result.UnitPrice, &result.Total)
	if err == nil {
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	if o.PoolID == 0 {
		if err := tx.QueryRow(ctx, `SELECT id FROM v3_marketplace.blind_box_pools WHERE scope='standard' ORDER BY id LIMIT 1`).Scan(&o.PoolID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return result, ErrNotFound
			}
			return result, err
		}
		if _, err := tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_orders SET pool_id=$2 WHERE id=$1 AND pool_id IS NULL`, o.ID, o.PoolID); err != nil {
			return result, err
		}
	}
	p, err := loadPool(ctx, tx, o.PoolID)
	if err != nil {
		return result, err
	}
	rewards, err := json.Marshal(p.Rewards)
	if err != nil {
		return result, err
	}
	guarantees, err := json.Marshal(p.Guarantees)
	if err != nil {
		return result, err
	}
	result.Quantity = o.Quantity
	err = tx.QueryRow(ctx, `INSERT INTO v3_marketplace.blind_box_purchases
 (user_id,pool_id,quantity,unit_price_micro,is_grant,purchase_date,request_id,external_order_id,created_at)
 VALUES($1,$2,$3,0,$4,$5,$6,$7,$8) RETURNING id`, o.UserID, o.PoolID, o.Quantity, o.Source != "purchase",
		o.CreatedAt.In(s.cfg.Location).Format("2006-01-02"), fmt.Sprintf("external-order:%d", o.ID), o.ID, o.CreatedAt).Scan(&result.ID)
	if err != nil {
		return result, stateError(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO v3_marketplace.blind_box_items
 (purchase_id,owner_user_id,purchase_user_id,pool_id,rewards,guarantees,draw_current_pool,created_at,updated_at,expires_at)
 SELECT $1,$2,$2,$3,$4,$5,true,$7,$7,$8 FROM generate_series(1,$6::int)`, result.ID, o.UserID, o.PoolID,
		rewards, guarantees, o.Quantity-o.OpenedCount, s.cfg.Now(), o.ExpiresAt)
	return result, err
}

// RecordExternalOpenTx must run with the inventory item's lock before its
// opened transition. It preserves legacy order counters and rejects expired
// grants even if inventory was materialized before the expiry boundary.
func (s *Service) RecordExternalOpenTx(ctx context.Context, tx pgx.Tx, purchaseID, recordID int64) error {
	var orderID int64
	err := tx.QueryRow(ctx, `SELECT coalesce(external_order_id,0) FROM v3_marketplace.blind_box_purchases WHERE id=$1`, purchaseID).Scan(&orderID)
	if err != nil {
		return err
	}
	if orderID == 0 {
		return nil
	}
	var existingOrderID int64
	if err := tx.QueryRow(ctx, `SELECT coalesce(order_id,0) FROM v3_marketplace.blind_box_open_records WHERE id=$1 FOR UPDATE`, recordID).Scan(&existingOrderID); err != nil {
		return err
	}
	if existingOrderID != 0 {
		if existingOrderID != orderID {
			return ErrConflict
		}
		return nil
	}
	// Bonus draws share the purchase but do not increase the paid quantity.
	tag, err := tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_orders SET opened_count=least(opened_count+1,quantity)
 WHERE id=$1 AND status IN('success','completed') AND (expires_at IS NULL OR expires_at>$2)`, orderID, s.cfg.Now())
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	tag, err = tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_open_records SET order_id=$2 WHERE id=$1 AND order_id IS NULL`, recordID, orderID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}
