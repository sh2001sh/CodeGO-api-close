package commerce

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// ResetDueSubscriptions drains old admissions before rotating the account.
// Each subscription is locked by one worker; an unfinished stream leaves its
// closed bucket pending and the next run retries after settlement.
func (s *Service) ResetDueSubscriptions(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	count := 0
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id FROM v3_commerce.subscriptions
		WHERE state='active' AND starts_at<=$1 AND expires_at>$1 AND next_reset_at<=$1
		AND NOT EXISTS(SELECT 1 FROM v3_commerce.package_checkouts pc WHERE pc.target_subscription_id=v3_commerce.subscriptions.id AND pc.state IN ('preparing','checkout')) AND NOT EXISTS(SELECT 1 FROM v3_commerce.subscription_operations op WHERE op.subscription_id=v3_commerce.subscriptions.id AND op.kind IN ('conversion','invalidate','delete') AND op.state='pending')
		ORDER BY next_reset_at,id LIMIT $2 FOR UPDATE SKIP LOCKED`, s.cfg.Now(), limit)
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
			if err = s.resetSubscriptionTx(ctx, tx, id, false, ""); err != nil {
				if errors.Is(err, ErrFundingPending) {
					continue
				}
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

func (s *Service) resetSubscriptionTx(ctx context.Context, tx pgx.Tx, id int64, manual bool, operation string) error {
	sub, err := s.lockSubscriptionForResetTx(ctx, tx, id, manual)
	if err != nil || sub == nil {
		return err
	}
	used, err := s.tallySubscriptionResetUsageTx(ctx, tx, id, *sub, manual)
	if err != nil {
		return err
	}
	remaining := max(sub.total-used, 0)
	if sub.total == 0 {
		remaining = sub.period
	}
	grant := remaining
	if sub.period > 0 {
		grant = min(grant, sub.period)
	}
	last := s.cfg.Now()
	next := nextReset(last, sub.rule, sub.seconds, sub.end)
	if !manual {
		last, next = advanceReset(*sub.due, sub.rule, sub.seconds, s.cfg.Now(), sub.end)
		operation = fmt.Sprintf("periodic:%d", last.Unix())
	}
	newAccount, err := s.rotateSubscriptionBucket(ctx, tx, id, sub.user, sub.account, grant, operation, last)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE v3_commerce.subscriptions SET account_id=$2,used_credits=$3,period_used=0,model_usage='{}'::jsonb,
	 last_reset_at=$4,next_reset_at=$5,reset_opportunity_used=reset_opportunity_used OR $6 WHERE id=$1`, id, newAccount, int64(used), last, next, manual)
	return err
}

type subscriptionResetRow struct {
	account, user       int64
	total, used, period credits.Micro
	legacy              bool
	end                 time.Time
	due                 *time.Time
	rule                string
	seconds             int64
	renewable           credits.Micro
	policyVersion       string
}

// lockSubscriptionForResetTx locks the subscription row and validates it is
// eligible to reset now. A nil row with a nil error means the reset should
// be silently skipped (not yet due).
func (s *Service) lockSubscriptionForResetTx(ctx context.Context, tx pgx.Tx, id int64, manual bool) (*subscriptionResetRow, error) {
	var row subscriptionResetRow
	err := tx.QueryRow(ctx, `SELECT account_id,user_id,total_credits,used_credits,period_credits,legacy_periodic,
	 expires_at,next_reset_at,reset_period,reset_custom_seconds,renewable_credits,policy_version FROM v3_commerce.subscriptions
	 WHERE id=$1 AND state='active' AND starts_at<=$2 AND expires_at>$2 FOR UPDATE`, id, s.cfg.Now()).
		Scan(&row.account, &row.user, &row.total, &row.used, &row.period, &row.legacy, &row.end, &row.due, &row.rule, &row.seconds, &row.renewable, &row.policyVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if row.policyVersion == PolicyStandardV2 {
		return nil, ErrStateConflict
	}
	if !manual && (row.due == nil || row.due.After(s.cfg.Now())) {
		return nil, nil
	}
	if err = s.checkPackagePending(ctx, tx, id); err != nil {
		return nil, err
	}
	if manual {
		var converted bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_commerce.subscription_conversions c JOIN v3_commerce.subscriptions s ON s.id=c.subscription_id WHERE c.subscription_id=$1 AND c.cycle_order_id=COALESCE(s.order_id,0))`, id).Scan(&converted); err != nil {
			return nil, err
		}
		if converted {
			return nil, ErrStateConflict
		}
	}
	return &row, nil
}

// tallySubscriptionResetUsageTx drains funding, tallies ledger spending onto
// the stored used_credits, and applies the legacy/manual renewable-credit
// carve-out so the computed usage reflects only billable consumption.
func (s *Service) tallySubscriptionResetUsageTx(ctx context.Context, tx pgx.Tx, id int64, sub subscriptionResetRow, manual bool) (credits.Micro, error) {
	if s.cfg.FundingDrain != nil {
		drained, err := s.cfg.FundingDrain.FreezeAndDrained(ctx, tx, sub.account)
		if err != nil {
			return 0, err
		}
		if !drained {
			return 0, ErrFundingPending
		}
	}
	var spent credits.Micro
	if err := tx.QueryRow(ctx, `SELECT COALESCE(GREATEST(-SUM(amount),0),0)::bigint FROM v3_billing.ledger_entries
	 WHERE account_id=$1 AND kind IN ('usage','refund')`, sub.account).Scan(&spent); err != nil {
		return 0, err
	}
	if sub.total > 0 && spent > sub.total-sub.used && !sub.legacy {
		return 0, fmt.Errorf("commerce: subscription %d lifetime spending exceeds allowance", id)
	}
	used, err := sub.used.Add(spent)
	if err != nil {
		return 0, err
	}
	if sub.legacy || manual {
		used = max(used-sub.renewable, 0)
	}
	if sub.total == 0 && manual {
		used = 0
	}
	return used, nil
}
