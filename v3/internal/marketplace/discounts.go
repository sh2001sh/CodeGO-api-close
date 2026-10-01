package marketplace

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Discounts implements the commerce-side port. Its writes use the caller's
// order transaction; reserving a card cannot survive a failed order creation.
type Discounts struct{ now func() time.Time }
type Discount struct {
	PropID  int64
	RatePPM int64
}

func NewDiscounts(now func() time.Time) *Discounts {
	if now == nil {
		now = time.Now
	}
	return &Discounts{now: now}
}

func (d *Discounts) ReserveDiscountTx(ctx context.Context, tx pgx.Tx, userID int64, kind, tradeNo string) (Discount, error) {
	var result Discount
	if userID <= 0 || tradeNo == "" || (kind != "topup" && kind != "subscription") {
		return result, ErrInvalidInput
	}
	if err := lockUser(ctx, tx, userID); err != nil {
		return result, err
	}
	var owner int64
	err := tx.QueryRow(ctx, `SELECT id,user_id,discount_rate_ppm FROM v3_marketplace.blind_box_props WHERE status IN('reserved','used') AND reserved_order_type=$1 AND reserved_order_trade_no=$2 FOR UPDATE`, kind, tradeNo).Scan(&result.PropID, &owner, &result.RatePPM)
	if err == nil {
		if owner != userID {
			return result, ErrConflict
		}
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	err = tx.QueryRow(ctx, `SELECT id,discount_rate_ppm FROM v3_marketplace.blind_box_props WHERE user_id=$1 AND status='available' AND (expires_at IS NULL OR expires_at>$3) AND (kind='topup_discount' OR ($2='subscription' AND kind='subscription_discount')) ORDER BY discount_rate_ppm DESC,id LIMIT 1 FOR UPDATE`, userID, kind, d.now()).Scan(&result.PropID, &result.RatePPM)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	tag, err := tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_props SET status='reserved',reserved_order_type=$2,reserved_order_trade_no=$3,reserved_at=$4,updated_at=$4 WHERE id=$1 AND status='available'`, result.PropID, kind, tradeNo, d.now())
	if err != nil {
		return result, err
	}
	if tag.RowsAffected() != 1 {
		return result, ErrConflict
	}
	return result, nil
}

func (d *Discounts) ConsumeDiscountTx(ctx context.Context, tx pgx.Tx, userID, propID int64, kind, tradeNo string) error {
	if propID == 0 {
		return nil
	}
	if userID <= 0 || tradeNo == "" || (kind != "topup" && kind != "subscription") {
		return ErrInvalidInput
	}
	var status, order, orderKind string
	var owner int64
	err := tx.QueryRow(ctx, `SELECT user_id,status,reserved_order_trade_no,reserved_order_type FROM v3_marketplace.blind_box_props WHERE id=$1 FOR UPDATE`, propID).Scan(&owner, &status, &order, &orderKind)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if owner != userID || order != tradeNo || orderKind != kind {
		return ErrConflict
	}
	if status == "used" {
		return nil
	}
	if status != "reserved" {
		return ErrConflict
	}
	tag, err := tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_props SET status='used',used_at=$3,updated_at=$3 WHERE id=$1 AND user_id=$2 AND status='reserved' AND reserved_order_type=$4 AND reserved_order_trade_no=$5`, propID, userID, d.now(), kind, tradeNo)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}

func (d *Discounts) ReleaseDiscountTx(ctx context.Context, tx pgx.Tx, userID, propID int64, kind, tradeNo string) error {
	if propID == 0 {
		return nil
	}
	if userID <= 0 || tradeNo == "" || (kind != "topup" && kind != "subscription") {
		return ErrInvalidInput
	}
	var owner int64
	var status, order, orderKind string
	err := tx.QueryRow(ctx, `SELECT user_id,status,reserved_order_trade_no,reserved_order_type FROM v3_marketplace.blind_box_props WHERE id=$1 FOR UPDATE`, propID).Scan(&owner, &status, &order, &orderKind)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if owner != userID {
		return ErrConflict
	}
	if status == "available" && order == "" {
		return nil
	}
	if status != "reserved" || order != tradeNo || orderKind != kind {
		return ErrConflict
	}
	_, err = tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_props SET status='available',reserved_at=NULL,reserved_order_type='',reserved_order_trade_no='',updated_at=$3 WHERE id=$1 AND user_id=$2 AND status='reserved' AND reserved_order_type=$4 AND reserved_order_trade_no=$5`, propID, userID, d.now(), kind, tradeNo)
	return err
}

func (s *Service) ConvertDiscountProp(ctx context.Context, userID, propID int64) (Prop, error) {
	var result Prop
	if userID <= 0 || propID <= 0 {
		return result, ErrInvalidInput
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockUser(ctx, tx, userID); err != nil {
			return err
		}
		var kind, status string
		if err := tx.QueryRow(ctx, `SELECT kind,status FROM v3_marketplace.blind_box_props WHERE id=$1 AND user_id=$2 FOR UPDATE`, propID, userID).Scan(&kind, &status); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if status != "available" || (kind != "subscription_discount" && kind != "topup_discount") {
			return ErrConflict
		}
		_, err := tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_props SET kind='topup_discount',prop_type='topup_discount_90',title='九折充值卡',discount_rate_ppm=100000,updated_at=$3 WHERE id=$1 AND user_id=$2 AND status='available'`, propID, userID, s.cfg.Now())
		result = Prop{ID: propID, Kind: "topup_discount", Title: "九折充值卡", Status: "available", DiscountRatePPM: 100000}
		return err
	})
	return result, err
}
