package commerce

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/marketplace"
)

// GroupCheckoutMarket enrolls a verified purchase in the payment transaction.
type GroupCheckoutMarket interface {
	CreateGroupTx(context.Context, pgx.Tx, int64, int64) (marketplace.Group, error)
	JoinGroupTx(context.Context, pgx.Tx, int64, int64, int64) (marketplace.Group, error)
}

func (s *Service) SetGroupCheckoutMarket(market GroupCheckoutMarket) { s.cfg.GroupCheckouts = market }

func isGroupPurchase(kind string) bool { return kind == "group_buy" || kind == "join_group" }

// PrepareGroupCheckoutTx runs before an immutable order is inserted. Normal
// purchases of eligible plans automatically enter the shared group pool.
func (s *Service) PrepareGroupCheckoutTx(ctx context.Context, tx pgx.Tx, o *Order, groupID int64) error {
	if o == nil || tx == nil {
		return ErrInvalid
	}
	if o.Kind != "subscription" || o.PurchaseType == "fuel" {
		if isGroupPurchase(o.PurchaseType) || groupID != 0 {
			return ErrInvalid
		}
		return nil
	}
	dayPass := o.DurationUnit == "day" && o.DurationValue > 0 && o.DurationValue <= 2
	eligible := o.GroupBuyEnabled && !dayPass
	if (o.PurchaseType == "" || o.PurchaseType == "normal") && eligible {
		o.PurchaseType = "group_buy"
	}
	if !isGroupPurchase(o.PurchaseType) {
		return nil
	}
	if !eligible || o.PlanID == nil || (o.PurchaseType == "join_group" && groupID <= 0) {
		return ErrInvalid
	}
	if s.cfg.GroupCheckouts == nil {
		return ErrProviderUnavailable
	}
	_, err := s.selectCheckoutGroupTx(ctx, tx, *o, groupID)
	return err
}

// SaveGroupCheckoutTx binds the target to an owned order rather than accepting
// a callback-supplied room. Auto grouping resolves its room at confirmation.
func (s *Service) SaveGroupCheckoutTx(ctx context.Context, tx pgx.Tx, o Order, groupID int64) error {
	if !isGroupPurchase(o.PurchaseType) {
		return nil
	}
	if o.PurchaseType == "group_buy" {
		groupID = 0
	}
	if o.ID <= 0 || o.UserID <= 0 || (o.PurchaseType == "join_group" && groupID <= 0) {
		return ErrInvalid
	}
	_, err := tx.Exec(ctx, `INSERT INTO v3_commerce.group_checkouts(order_id,user_id,purchase_type,requested_group_id)
	 VALUES($1,$2,$3,NULLIF($4,0))`, o.ID, o.UserID, o.PurchaseType, groupID)
	return err
}

// MatchGroupCheckoutTx protects a retried package request from changing rooms.
func (s *Service) MatchGroupCheckoutTx(ctx context.Context, tx pgx.Tx, o Order, kind string, groupID int64) error {
	if kind == "" || kind == "normal" {
		kind = o.PurchaseType
	}
	if kind != "join_group" {
		groupID = 0
	}
	var savedKind string
	var savedID int64
	err := tx.QueryRow(ctx, `SELECT purchase_type,COALESCE(requested_group_id,0) FROM v3_commerce.group_checkouts WHERE order_id=$1`, o.ID).Scan(&savedKind, &savedID)
	if errors.Is(err, pgx.ErrNoRows) {
		if isGroupPurchase(kind) || isGroupPurchase(o.PurchaseType) {
			return ErrStateConflict
		}
		return nil
	}
	if err != nil {
		return err
	}
	if kind != savedKind || groupID != savedID {
		return ErrStateConflict
	}
	return nil
}

func (s *Service) selectCheckoutGroupTx(ctx context.Context, tx pgx.Tx, o Order, requested int64) (int64, error) {
	if o.PlanID == nil {
		return 0, ErrInvalid
	}
	var id, plan int64
	var joinable bool
	if o.PurchaseType == "join_group" {
		err := tx.QueryRow(ctx, `SELECT id,plan_id,status='pending' AND expires_at>$2 AND current_count<target_count
		 FROM v3_marketplace.group_buys WHERE id=$1 FOR UPDATE`, requested, s.cfg.Now()).Scan(&id, &plan, &joinable)
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrNotFound
		}
		if err != nil {
			return 0, err
		}
		if plan != *o.PlanID || !joinable {
			return 0, ErrStateConflict
		}
	} else {
		err := tx.QueryRow(ctx, `SELECT id FROM v3_marketplace.group_buys
		 WHERE plan_id=$1 AND status='pending' AND expires_at>$2 AND current_count<target_count ORDER BY id LIMIT 1 FOR UPDATE`, *o.PlanID, s.cfg.Now()).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		if err != nil {
			return 0, err
		}
	}
	var member bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_marketplace.group_buy_members WHERE group_buy_id=$1 AND user_id=$2)`, id, o.UserID).Scan(&member); err != nil {
		return 0, err
	}
	if member {
		return 0, ErrStateConflict
	}
	return id, nil
}
