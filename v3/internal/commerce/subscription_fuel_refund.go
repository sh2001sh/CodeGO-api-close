package commerce

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// RevokeSubscriptionFuelTx follows a verified full refund. Remaining benefits
// are reduced in the original cycle; a later renewed cycle is unaffected.
func (s *Service) RevokeSubscriptionFuelTx(ctx context.Context, tx pgx.Tx, o Order) error {
	if err := lockSubscriptionUserTx(ctx, tx, o.TargetSubscriptionID); err != nil {
		return err
	}
	var revoked bool
	err := tx.QueryRow(ctx, `SELECT revoked FROM v3_commerce.subscription_fuel_fulfillments WHERE order_id=$1 FOR UPDATE`, o.ID).Scan(&revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil || revoked {
		return err
	}
	if handled, e := s.refundConvertedSubscriptionTx(ctx, tx, o); handled || e != nil {
		if e != nil {
			return e
		}
		_, err = tx.Exec(ctx, `UPDATE v3_commerce.subscription_fuel_fulfillments SET revoked=true WHERE order_id=$1`, o.ID)
		return err
	}
	var account int64
	var total, used, periodUsed credits.Micro
	var end time.Time
	var state string
	err = tx.QueryRow(ctx, `SELECT account_id,total_credits,used_credits,period_used,expires_at,state FROM v3_commerce.subscriptions WHERE id=$1 AND user_id=$2 FOR UPDATE`, o.TargetSubscriptionID, o.UserID).Scan(&account, &total, &used, &periodUsed, &end, &state)
	if err != nil {
		return err
	}
	if o.FuelExpiresAt != nil && end.Equal(*o.FuelExpiresAt) && state == "active" {
		if err = s.revokeSubscriptionFuelGrantTx(ctx, tx, o, account, total, used, periodUsed); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE v3_commerce.subscription_fuel_fulfillments SET revoked=true WHERE order_id=$1`, o.ID)
	return err
}

// revokeSubscriptionFuelGrantTx removes the fuel order's credit grant from
// the subscription's remaining allowance, canceling the subscription if that
// exhausts it, since this cycle's fuel top-up is being reversed.
func (s *Service) revokeSubscriptionFuelGrantTx(ctx context.Context, tx pgx.Tx, o Order, account int64, total, used, periodUsed credits.Micro) error {
	if err := s.checkPackagePending(ctx, tx, o.TargetSubscriptionID); err != nil {
		return err
	}
	if s.cfg.FundingDrain != nil {
		ready, err := s.cfg.FundingDrain.FreezeAndDrained(ctx, tx, account)
		if err != nil {
			return err
		}
		if !ready {
			return ErrFundingPending
		}
	}
	var spent credits.Micro
	if err := tx.QueryRow(ctx, `SELECT COALESCE(GREATEST(-SUM(amount),0),0)::bigint FROM v3_billing.ledger_entries WHERE account_id=$1 AND kind IN ('usage','refund')`, account).Scan(&spent); err != nil {
		return err
	}
	var err error
	used, err = used.Add(spent)
	if err != nil {
		return err
	}
	periodUsed, err = periodUsed.Add(spent)
	if err != nil {
		return err
	}
	var balance credits.Micro
	if err = tx.QueryRow(ctx, `SELECT balance FROM v3_billing.accounts WHERE id=$1 FOR UPDATE`, account).Scan(&balance); err != nil {
		return err
	}
	total = max(total-o.Credits, 0)
	grant := min(max(balance, 0), max(total-used, 0))
	state := "active"
	if total == 0 || used >= total {
		state = "canceled"
		used = min(used, total)
		grant = 0
	}
	fresh, err := s.rotateSubscriptionBucket(ctx, tx, o.TargetSubscriptionID, o.UserID, account, grant, "fuel-refund:"+o.TradeNo, s.cfg.Now())
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE v3_commerce.subscriptions SET total_credits=$2,used_credits=$3,period_used=$4,account_id=$5,state=$6,ended_at=CASE WHEN $6='canceled' THEN $7::timestamptz ELSE ended_at END WHERE id=$1`, o.TargetSubscriptionID, int64(total), int64(used), int64(periodUsed), fresh, state, s.cfg.Now()); err != nil {
		return err
	}
	if state == "canceled" {
		return RestoreSubscriptionGroupTx(ctx, tx, o.TargetSubscriptionID, s.cfg.Now())
	}
	return nil
}
