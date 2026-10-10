package legacy

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/jackc/pgx/v5"
)

type retiredUsageTotal struct{ UserID, KeyID, Amount int64 }

func loadRetiredUsageTotals(ctx context.Context, source pgx.Tx, logs string) ([]retiredUsageTotal, error) {
	if historyCutoffFrom(ctx).IsZero() || logs == "" {
		return nil, nil
	}
	rows, err := source.Query(ctx, "SELECT user_id,token_id,coalesce(sum(quota),0)::text,bool_or(quota IS NULL OR quota<0) FROM "+logs+" l WHERE type=2 AND NOT "+historyWindow(ctx, "logs", "l", "", "")+" GROUP BY user_id,token_id ORDER BY user_id,token_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var totals []retiredUsageTotal
	for rows.Next() {
		var row retiredUsageTotal
		var units string
		var invalid bool
		if err := rows.Scan(&row.UserID, &row.KeyID, &units, &invalid); err != nil {
			return nil, err
		}
		value, ok := new(big.Int).SetString(units, 10)
		if !ok || invalid || row.UserID < 0 || row.KeyID < 0 {
			return nil, errors.New("legacy: invalid retired Key usage aggregate")
		}
		value.Mul(value, big.NewInt(2))
		if !value.IsInt64() {
			return nil, errors.New("legacy: retired Key usage overflows micro-credits")
		}
		row.Amount = value.Int64()
		totals = append(totals, row)
	}
	return totals, rows.Err()
}

func importRetiredUsageTotals(ctx context.Context, target pgx.Tx, totals []retiredUsageTotal) error {
	if historyCutoffFrom(ctx).IsZero() {
		return checkRetiredUsageTotals(ctx, target, nil)
	}
	for _, row := range totals {
		if _, err := target.Exec(ctx, "INSERT INTO v3_billing.retired_usage_totals(user_id,key_id,amount) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", row.UserID, row.KeyID, row.Amount); err != nil {
			return err
		}
	}
	return checkRetiredUsageTotals(ctx, target, totals)
}

func checkRetiredUsageTotals(ctx context.Context, target pgx.Tx, totals []retiredUsageTotal) error {
	// Compact totals are installed and checked atomically by frozen final Import.
	if _, staged := target.(onlineStageTx); staged {
		return nil
	}
	if historyCutoffFrom(ctx).IsZero() {
		var exists bool
		if err := target.QueryRow(ctx, "SELECT to_regclass('v3_billing.retired_usage_totals') IS NOT NULL").Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return nil // Explicitly unfiltered predecessor fixtures.
		}
	}
	var count int
	if err := target.QueryRow(ctx, "SELECT count(*) FROM v3_billing.retired_usage_totals").Scan(&count); err != nil {
		return err
	}
	if count != len(totals) {
		return errors.New("legacy: retired Key usage aggregate count differs")
	}
	for _, row := range totals {
		var amount int64
		if err := target.QueryRow(ctx, "SELECT amount FROM v3_billing.retired_usage_totals WHERE user_id=$1 AND key_id=$2", row.UserID, row.KeyID).Scan(&amount); err != nil {
			return err
		}
		if amount != row.Amount {
			return fmt.Errorf("legacy: retired Key usage differs for key %d", row.KeyID)
		}
	}
	return nil
}
