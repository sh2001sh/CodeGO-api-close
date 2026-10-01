package commerce

import (
	"context"
	"errors"
	"math/big"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/marketplace"
)

type CheckoutDiscount struct {
	OrderID       int64  `json:"order_id"`
	OriginalMinor int64  `json:"original_minor"`
	OriginalKnown bool   `json:"original_known"`
	PaidMinor     int64  `json:"paid_minor"`
	Campaign      bool   `json:"first_purchase_discount"`
	Multiplier    string `json:"multiplier"`
	StartsAt      int64  `json:"starts_at"`
	EndsAt        int64  `json:"ends_at"`
	PropID        *int64 `json:"prop_id,omitempty"`
	State         string `json:"state"`
}

const checkoutDiscountColumns = `order_id,original_minor,original_known,paid_minor,campaign,multiplier,starts_at,ends_at,prop_id,state`

// ApplyCheckoutDiscountTx runs after inserting a normal order or after computing
// a renewal quote, in that same transaction. Credit grants remain unchanged.
func (s *Service) ApplyCheckoutDiscountTx(ctx context.Context, tx pgx.Tx, o *Order) error {
	if o == nil || o.ID <= 0 || o.UserID <= 0 || o.AmountMinor <= 0 || o.State != "created" {
		return ErrInvalid
	}
	if (o.Kind != "topup" && o.Kind != "subscription") || o.PurchaseType == "fuel" || o.PurchaseType == "subscription_fuel" {
		return nil
	}
	var user int64
	if err := tx.QueryRow(ctx, `SELECT id FROM v3_identity.users WHERE id=$1 FOR UPDATE`, o.UserID).Scan(&user); err != nil {
		return err
	}
	d, err := scanCheckoutDiscount(tx.QueryRow(ctx, `SELECT `+checkoutDiscountColumns+` FROM v3_commerce.checkout_discounts WHERE order_id=$1`, o.ID))
	if err == nil {
		o.AmountMinor = d.PaidMinor
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	d = CheckoutDiscount{OrderID: o.ID, OriginalMinor: o.AmountMinor, OriginalKnown: true, State: "reserved"}
	noDiscount, err := s.reserveCheckoutDiscountSourceTx(ctx, tx, *o, &d)
	if err != nil || noDiscount {
		return err
	}
	d.PaidMinor, err = discountMinor(d.OriginalMinor, d.Multiplier)
	if err != nil {
		return err
	}
	d.PaidMinor = max(d.PaidMinor, min(d.OriginalMinor, max(paymentScale(o.Currency)/100, 1)))
	_, err = tx.Exec(ctx, `INSERT INTO v3_commerce.checkout_discounts(order_id,original_minor,paid_minor,campaign,multiplier,starts_at,ends_at,prop_id,created_at,updated_at)
	 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$9)`, d.OrderID, d.OriginalMinor, d.PaidMinor, d.Campaign, d.Multiplier, d.StartsAt, d.EndsAt, d.PropID, s.cfg.Now())
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE v3_commerce.orders SET amount_minor=$2 WHERE id=$1 AND state='created'`, o.ID, d.PaidMinor)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrStateConflict
	}
	o.AmountMinor = d.PaidMinor
	return nil
}

// reserveCheckoutDiscountSourceTx picks the discount source for this order:
// the active first-purchase campaign if eligible, otherwise a marketplace
// prop discount. A true (noDiscount) return means neither applies and the
// caller should leave the order at its original price.
func (s *Service) reserveCheckoutDiscountSourceTx(ctx context.Context, tx pgx.Tx, o Order, d *CheckoutDiscount) (bool, error) {
	c, err := loadCheckoutCampaign(ctx, tx)
	if err != nil {
		return false, err
	}
	if c.active(s.cfg.Now().Unix()) {
		eligible, err := campaignEligibleTx(ctx, tx, o)
		if err != nil {
			return false, err
		}
		if eligible {
			d.Campaign = true
			d.Multiplier = c.multiplier
			d.StartsAt = c.start
			d.EndsAt = c.end
			return false, nil
		}
	}
	prop, err := marketplace.NewDiscounts(s.cfg.Now).ReserveDiscountTx(ctx, tx, o.UserID, o.Kind, o.TradeNo)
	if err != nil {
		return false, err
	}
	if prop.PropID == 0 {
		return true, nil
	}
	if prop.RatePPM <= 0 || prop.RatePPM > 1000000 {
		return false, ErrInvalid
	}
	d.PropID = &prop.PropID
	rate := prop.RatePPM
	if rate == 1000000 {
		rate = 990000
	}
	d.Multiplier = new(big.Rat).SetFrac(big.NewInt(1000000-rate), big.NewInt(1000000)).FloatString(6)
	return false, nil
}

func scanCheckoutDiscount(row scanner) (CheckoutDiscount, error) {
	var d CheckoutDiscount
	err := row.Scan(&d.OrderID, &d.OriginalMinor, &d.OriginalKnown, &d.PaidMinor, &d.Campaign, &d.Multiplier, &d.StartsAt, &d.EndsAt, &d.PropID, &d.State)
	return d, err
}

func (s *Service) GetCheckoutDiscount(ctx context.Context, userID int64, trade string) (CheckoutDiscount, error) {
	d, err := scanCheckoutDiscount(s.pool.QueryRow(ctx, `SELECT d.order_id,d.original_minor,d.original_known,d.paid_minor,d.campaign,d.multiplier,d.starts_at,d.ends_at,d.prop_id,d.state
	 FROM v3_commerce.checkout_discounts d JOIN v3_commerce.orders o ON o.id=d.order_id WHERE o.user_id=$1 AND o.trade_no=$2`, userID, trade))
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return d, err
}

// ConsumeCheckoutDiscountTx returns false for a late paid reservation that was
// released. The signed payment commits for operator review without benefits.
func (s *Service) ConsumeCheckoutDiscountTx(ctx context.Context, tx pgx.Tx, o Order) (bool, error) {
	d, err := scanCheckoutDiscount(tx.QueryRow(ctx, `SELECT `+checkoutDiscountColumns+` FROM v3_commerce.checkout_discounts WHERE order_id=$1 FOR UPDATE`, o.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if d.PaidMinor != o.AmountMinor {
		return false, ErrPaymentMismatch
	}
	if d.State == "consumed" {
		return true, nil
	}
	if d.State == "released" || d.State == "review" {
		return s.reviewCheckoutDiscountTx(ctx, tx, o.ID)
	}
	if d.PropID != nil {
		err = marketplace.NewDiscounts(s.cfg.Now).ConsumeDiscountTx(ctx, tx, o.UserID, *d.PropID, o.Kind, o.TradeNo)
		if errors.Is(err, marketplace.ErrConflict) || errors.Is(err, marketplace.ErrNotFound) {
			return s.reviewCheckoutDiscountTx(ctx, tx, o.ID)
		}
		if err != nil {
			return false, err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE v3_commerce.checkout_discounts SET state='consumed',updated_at=$2 WHERE order_id=$1`, o.ID, s.cfg.Now())
	return err == nil, err
}

func (s *Service) reviewCheckoutDiscountTx(ctx context.Context, tx pgx.Tx, id int64) (bool, error) {
	if _, err := tx.Exec(ctx, `UPDATE v3_commerce.checkout_discounts SET state='review',updated_at=$2 WHERE order_id=$1`, id, s.cfg.Now()); err != nil {
		return false, err
	}
	_, err := tx.Exec(ctx, `UPDATE v3_commerce.orders SET fulfillment_state='requires_review' WHERE id=$1`, id)
	return false, err
}

func (s *Service) CheckoutDiscountReviewTx(ctx context.Context, tx pgx.Tx, id int64) (bool, error) {
	var review bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_commerce.checkout_discounts WHERE order_id=$1 AND state='review')`, id).Scan(&review)
	return review, err
}

// ReleaseCheckoutDiscountTx needs the caller's locked order and terminal state.
func (s *Service) ReleaseCheckoutDiscountTx(ctx context.Context, tx pgx.Tx, o Order) error {
	if o.State != "canceled" && o.State != "expired" && o.State != "failed" {
		return ErrStateConflict
	}
	d, err := scanCheckoutDiscount(tx.QueryRow(ctx, `SELECT `+checkoutDiscountColumns+` FROM v3_commerce.checkout_discounts WHERE order_id=$1 FOR UPDATE`, o.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil || d.State != "reserved" {
		return err
	}
	if d.PropID != nil {
		if err = marketplace.NewDiscounts(s.cfg.Now).ReleaseDiscountTx(ctx, tx, o.UserID, *d.PropID, o.Kind, o.TradeNo); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE v3_commerce.checkout_discounts SET state='released',updated_at=$2 WHERE order_id=$1`, o.ID, s.cfg.Now())
	return err
}

func (s *Service) RecoverCheckoutDiscounts(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	count := 0
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT o.id FROM v3_commerce.orders o JOIN v3_commerce.checkout_discounts d ON d.order_id=o.id
		 WHERE d.state='reserved' AND o.state IN('failed','expired','canceled') ORDER BY o.id LIMIT $1 FOR UPDATE OF o SKIP LOCKED`, limit)
		if err != nil {
			return err
		}
		var ids []int64
		for rows.Next() {
			var id int64
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		for _, id := range ids {
			o, err := scanOrder(tx.QueryRow(ctx, `SELECT `+orderColumns+` FROM v3_commerce.orders WHERE id=$1`, id))
			if err != nil {
				return err
			}
			if err = s.ReleaseCheckoutDiscountTx(ctx, tx, o); err != nil {
				return err
			}
			count++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}
