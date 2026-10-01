package marketplace

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// BoxOrderInput is a server quote. AmountMinor and Currency must come from
// commerce's configured price, never directly from the checkout browser.
// ExpiresAt is the inventory's consumption deadline, not the checkout TTL.
type BoxOrderInput struct {
	UserID, PoolID, AmountMinor, SubscriptionID int64
	Quantity                                    int
	TradeNo, Currency, PaymentProvider          string
	PaymentMethod, Source, BenefitCycle         string
	ExpiresAt                                   *time.Time
}

type BoxOrder struct {
	BoxOrderInput
	ID          int64
	OpenedCount int
	Status      string
	CreatedAt   time.Time
	CompletedAt *time.Time
}

const boxOrderColumns = `id,user_id,coalesce(pool_id,0),quantity,opened_count,amount_minor,
 coalesce(subscription_id,0),trade_no,currency,payment_provider,payment_method,source,
 benefit_cycle,status,created_at,completed_at,expires_at`

func scanBoxOrder(row pgx.Row) (BoxOrder, error) {
	var o BoxOrder
	err := row.Scan(&o.ID, &o.UserID, &o.PoolID, &o.Quantity, &o.OpenedCount,
		&o.AmountMinor, &o.SubscriptionID, &o.TradeNo, &o.Currency,
		&o.PaymentProvider, &o.PaymentMethod, &o.Source, &o.BenefitCycle,
		&o.Status, &o.CreatedAt, &o.CompletedAt, &o.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, ErrNotFound
	}
	return o, err
}

func validBoxOrder(in BoxOrderInput) bool {
	if in.UserID <= 0 || in.PoolID <= 0 || in.Quantity < 1 || (in.Quantity > 100 && in.Source != "redemption") ||
		in.AmountMinor < 0 || in.SubscriptionID < 0 || len(in.TradeNo) == 0 || len(in.TradeNo) > 200 ||
		len(in.Currency) != 3 || len(in.BenefitCycle) > 200 {
		return false
	}
	for _, c := range in.Currency {
		if c < 'a' || c > 'z' {
			return false
		}
	}
	switch in.Source {
	case "purchase":
		return in.AmountMinor > 0 && in.PaymentProvider != ""
	case "admin_grant", "registration_benefit", "redemption":
		return in.AmountMinor == 0
	case "subscription_benefit":
		return in.AmountMinor == 0 && in.SubscriptionID > 0 && in.BenefitCycle != ""
	default:
		return false
	}
}

func sameBoxQuote(a, b BoxOrderInput) bool {
	timesEqual := a.ExpiresAt == nil && b.ExpiresAt == nil ||
		a.ExpiresAt != nil && b.ExpiresAt != nil && a.ExpiresAt.Equal(*b.ExpiresAt)
	return a.UserID == b.UserID && a.PoolID == b.PoolID && a.Quantity == b.Quantity &&
		a.AmountMinor == b.AmountMinor && a.SubscriptionID == b.SubscriptionID &&
		a.TradeNo == b.TradeNo && a.Currency == b.Currency &&
		a.PaymentProvider == b.PaymentProvider && a.PaymentMethod == b.PaymentMethod &&
		a.Source == b.Source && a.BenefitCycle == b.BenefitCycle && timesEqual
}

// CreateBoxOrderTx shares the payment order transaction; rollback cannot leave
// a separate pending inventory order behind. Retries retain the original quote.
func (s *Service) CreateBoxOrderTx(ctx context.Context, tx pgx.Tx, in BoxOrderInput) (BoxOrder, error) {
	if in.Source == "" {
		in.Source = "purchase"
	}
	in.Currency = strings.ToLower(in.Currency)
	if in.ExpiresAt != nil {
		deadline := in.ExpiresAt.Truncate(time.Microsecond)
		in.ExpiresAt = &deadline
	}
	if !validBoxOrder(in) {
		return BoxOrder{}, ErrInvalidInput
	}
	if err := lockUser(ctx, tx, in.UserID); err != nil {
		return BoxOrder{}, err
	}
	o, err := scanBoxOrder(tx.QueryRow(ctx, `SELECT `+boxOrderColumns+` FROM v3_marketplace.blind_box_orders WHERE trade_no=$1 FOR UPDATE`, in.TradeNo))
	if err == nil {
		if !sameBoxQuote(o.BoxOrderInput, in) {
			return o, ErrConflict
		}
		return o, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return o, err
	}
	if in.ExpiresAt != nil && !in.ExpiresAt.After(s.cfg.Now()) {
		return o, ErrInvalidInput
	}
	p, err := loadPool(ctx, tx, in.PoolID)
	if err != nil {
		return o, err
	}
	if !p.Enabled {
		return o, ErrUnavailable
	}
	if in.Source == "purchase" {
		if err := s.checkPurchaseLimitsTx(ctx, tx, in.UserID, p, in.Quantity); err != nil {
			return o, err
		}
	}
	o, err = scanBoxOrder(tx.QueryRow(ctx, `INSERT INTO v3_marketplace.blind_box_orders
 (user_id,pool_id,quantity,amount_minor,subscription_id,trade_no,currency,payment_provider,payment_method,source,benefit_cycle,expires_at,created_at)
 VALUES($1,$2,$3,$4,NULLIF($5,0),$6,$7,$8,$9,$10,$11,$12,$13) RETURNING `+boxOrderColumns,
		in.UserID, in.PoolID, in.Quantity, in.AmountMinor, in.SubscriptionID, in.TradeNo, in.Currency,
		in.PaymentProvider, in.PaymentMethod, in.Source, in.BenefitCycle, in.ExpiresAt, s.cfg.Now()))
	return o, stateError(err)
}

// CompleteBoxOrderTx runs after commerce verifies the provider, amount and
// currency. It repeats the amount check and creates sealed inventory atomically.
func (s *Service) CompleteBoxOrderTx(ctx context.Context, tx pgx.Tx, userID int64, tradeNo string, amountMinor int64, currency string) (Purchase, error) {
	var result Purchase
	if userID <= 0 || tradeNo == "" || amountMinor < 0 {
		return result, ErrInvalidInput
	}
	if err := lockUser(ctx, tx, userID); err != nil {
		return result, err
	}
	o, err := scanBoxOrder(tx.QueryRow(ctx, `SELECT `+boxOrderColumns+` FROM v3_marketplace.blind_box_orders WHERE trade_no=$1 FOR UPDATE`, tradeNo))
	if err != nil {
		return result, err
	}
	if o.UserID != userID || o.AmountMinor != amountMinor || o.Currency != strings.ToLower(currency) {
		return result, ErrConflict
	}
	if o.Status != "pending" && o.Status != "success" && o.Status != "completed" && (o.Status != "expired" || o.Source != "purchase") {
		return result, ErrConflict
	}
	if o.Status == "success" || o.Status == "completed" {
		err := tx.QueryRow(ctx, `SELECT id,quantity,unit_price_micro,total_micro FROM v3_marketplace.blind_box_purchases WHERE external_order_id=$1`, o.ID).
			Scan(&result.ID, &result.Quantity, &result.UnitPrice, &result.Total)
		if err == nil {
			return result, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return result, err
		}
	}
	if o.ExpiresAt != nil && !o.ExpiresAt.After(s.cfg.Now()) {
		return result, ErrUnavailable
	}
	result, err = s.materializeBoxOrderTx(ctx, tx, o)
	if err != nil {
		return result, err
	}
	_, err = tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_orders SET status='completed',completed_at=coalesce(completed_at,$2) WHERE id=$1`, o.ID, s.cfg.Now())
	return result, err
}

// CancelBoxOrderTx is also the refund revocation port. A consumed or transferred
// order cannot be refunded; commerce must fail its refund in the same transaction.
func (s *Service) CancelBoxOrderTx(ctx context.Context, tx pgx.Tx, userID int64, tradeNo string) error {
	if userID <= 0 || tradeNo == "" {
		return ErrInvalidInput
	}
	if err := lockUser(ctx, tx, userID); err != nil {
		return err
	}
	o, err := scanBoxOrder(tx.QueryRow(ctx, `SELECT `+boxOrderColumns+` FROM v3_marketplace.blind_box_orders WHERE trade_no=$1 FOR UPDATE`, tradeNo))
	if err != nil {
		return err
	}
	if o.UserID != userID {
		return ErrConflict
	}
	if o.Status == "refunded" || o.Status == "cancelled" || o.Status == "canceled" || o.Status == "revoked" {
		return nil
	}
	if o.OpenedCount != 0 {
		return ErrConflict
	}
	var consumed bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_marketplace.blind_box_items i
 JOIN v3_marketplace.blind_box_purchases p ON p.id=i.purchase_id WHERE p.external_order_id=$1
 AND (i.status='opened' OR i.owner_user_id<>$2))`, o.ID, userID).Scan(&consumed); err != nil {
		return err
	}
	if consumed {
		return ErrConflict
	}
	if _, err := tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_items SET status='revoked',updated_at=$2
 WHERE purchase_id IN(SELECT id FROM v3_marketplace.blind_box_purchases WHERE external_order_id=$1) AND status='available'`, o.ID, s.cfg.Now()); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_purchases SET status='revoked' WHERE external_order_id=$1`, o.ID); err != nil {
		return err
	}
	status := "cancelled"
	if o.Status == "success" || o.Status == "completed" {
		status = "refunded"
	}
	_, err = tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_orders SET status=$2 WHERE id=$1`, o.ID, status)
	return err
}
