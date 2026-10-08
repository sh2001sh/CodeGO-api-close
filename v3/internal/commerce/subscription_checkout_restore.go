package commerce

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func (s *Service) RestorePackageCheckout(ctx context.Context, orderID int64) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var orderState, fulfillment string
		if err := tx.QueryRow(ctx, `SELECT state,fulfillment_state FROM v3_commerce.orders WHERE id=$1 FOR UPDATE`, orderID).Scan(&orderState, &fulfillment); err != nil {
			return err
		}
		var id, account int64
		var state string
		err := tx.QueryRow(ctx, `SELECT target_subscription_id,source_account_id,state FROM v3_commerce.package_checkouts WHERE order_id=$1 FOR UPDATE`, orderID).Scan(&id, &account, &state)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if state == "applied" || state == "restored" {
			return nil
		}
		terminal := orderState == "failed" || orderState == "canceled" || orderState == "expired"
		unfulfilledPayment := (orderState == "paid" || orderState == "refunded") && fulfillment == "requires_review"
		if !terminal && !unfulfilledPayment {
			return ErrStateConflict
		}
		if terminal {
			o, e := scanOrder(tx.QueryRow(ctx, `SELECT `+orderColumns+` FROM v3_commerce.orders WHERE id=$1`, orderID))
			if e != nil {
				return e
			}
			if e = s.releaseCheckoutTx(ctx, tx, o); e != nil {
				return e
			}
		}
		if err = s.restorePackageCheckoutSubscriptionTx(ctx, tx, orderID, id, account); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE v3_commerce.package_checkouts SET state='restored' WHERE order_id=$1`, orderID)
		return err
	})
}

// restorePackageCheckoutSubscriptionTx rolls the subscription's bucket back
// to its current (post-drain) balance, since the pending package renewal is
// being abandoned.
func (s *Service) restorePackageCheckoutSubscriptionTx(ctx context.Context, tx pgx.Tx, orderID, id, account int64) error {
	var user, actual int64
	var used, periodUsed, spent, balance credits.Micro
	var expires time.Time
	var subState string
	if err := tx.QueryRow(ctx, `SELECT user_id,account_id,used_credits,period_used,expires_at,state FROM v3_commerce.subscriptions WHERE id=$1 FOR UPDATE`, id).Scan(&user, &actual, &used, &periodUsed, &expires, &subState); err != nil {
		return err
	}
	if actual != account {
		return ErrStateConflict
	}
	if s.cfg.FundingDrain != nil {
		drained, err := s.cfg.FundingDrain.FreezeAndDrained(ctx, tx, account)
		if err != nil {
			return err
		}
		if !drained {
			return ErrFundingPending
		}
	}
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
	if err = tx.QueryRow(ctx, `SELECT balance FROM v3_billing.accounts WHERE id=$1 FOR UPDATE`, account).Scan(&balance); err != nil {
		return err
	}
	grant := max(balance, 0)
	if !expires.After(s.cfg.Now()) || subState != "active" {
		grant = 0
	}
	newAccount, err := s.rotateSubscriptionBucket(ctx, tx, id, user, account, grant, "restore:"+strconv.FormatInt(orderID, 10), s.cfg.Now())
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE v3_commerce.subscriptions SET account_id=$2,used_credits=$3,period_used=$4 WHERE id=$1`, id, newAccount, int64(used), int64(periodUsed))
	return err
}

func (s *Service) RecoverPackageCheckouts(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `SELECT pc.order_id FROM v3_commerce.package_checkouts pc JOIN v3_commerce.orders o ON o.id=pc.order_id
	 WHERE pc.state IN ('preparing','checkout') AND (o.state IN ('canceled','failed','expired') OR (o.state IN ('paid','refunded') AND o.fulfillment_state='requires_review') OR o.expires_at<=$1 OR o.payment_url='') ORDER BY pc.created_at LIMIT $2`, s.cfg.Now(), limit)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return 0, err
	}
	count := 0
	for _, id := range ids {
		if _, err = s.pool.Exec(ctx, `UPDATE v3_commerce.orders SET state='expired' WHERE id=$1 AND state='created' AND expires_at<=$2`, id, s.cfg.Now()); err != nil {
			return count, err
		}
		var state string
		if err = s.pool.QueryRow(ctx, `SELECT state FROM v3_commerce.orders WHERE id=$1`, id).Scan(&state); err != nil {
			return count, err
		}
		if state == "created" {
			_, err = s.resumePackageCheckout(ctx, id)
		} else {
			err = s.RestorePackageCheckout(ctx, id)
		}
		if errors.Is(err, ErrFundingPending) {
			continue
		}
		if errors.Is(err, ErrStateConflict) {
			var restored bool
			if readErr := s.pool.QueryRow(ctx, `SELECT state='restored' FROM v3_commerce.package_checkouts WHERE order_id=$1`, id).Scan(&restored); readErr != nil {
				return count, readErr
			}
			if restored {
				count++
				continue
			}
		}
		if err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}
