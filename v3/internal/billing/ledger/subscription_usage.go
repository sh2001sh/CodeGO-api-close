package ledger

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// Persist actual model debits before locking account balances. Redis accounts
// rotate at reset, so imported model usage and the current period never share
// a stale counter with a newly issued subscription bucket. Only fresh ledger
// events reach this function; key-budget mirrors have no subscription mapping.
func recordSubscriptionModelUsageTx(ctx context.Context, tx pgx.Tx, events []event) error {
	changes, err := subscriptionModelUsageChanges(events)
	if err != nil {
		return err
	}
	if len(changes) == 0 {
		return nil
	}
	updates, err := lockSubscriptionUsageUpdatesTx(ctx, tx, changes)
	if err != nil {
		return err
	}
	for _, u := range updates {
		if _, err := tx.Exec(ctx, `UPDATE v3_commerce.subscriptions SET model_usage=$2 WHERE id=$1`, u.id, u.usage); err != nil {
			return err
		}
	}
	return nil
}

// subscriptionModelUsageChanges sums per-account, per-model deltas from
// events, skipping zero-amount or model-less events (key-budget mirrors).
func subscriptionModelUsageChanges(events []event) (map[int64]map[string]credits.Micro, error) {
	changes := map[int64]map[string]credits.Micro{}
	for _, e := range events {
		model := e.fields[billing.FieldModel]
		if e.amount == 0 || model == "" {
			continue
		}
		if changes[e.accountID] == nil {
			changes[e.accountID] = map[string]credits.Micro{}
		}
		next, err := changes[e.accountID][model].Add(credits.Micro(e.amount))
		if err != nil {
			return nil, err
		}
		changes[e.accountID][model] = next
	}
	return changes, nil
}

type subscriptionUsageUpdate struct {
	id    int64
	usage map[string]int64
}

// lockSubscriptionUsageUpdatesTx locks each affected account's subscription
// row and applies changes to its model_usage map, returning the rows to
// persist.
func lockSubscriptionUsageUpdatesTx(ctx context.Context, tx pgx.Tx, changes map[int64]map[string]credits.Micro) ([]subscriptionUsageUpdate, error) {
	ids := make([]int64, 0, len(changes))
	for id := range changes {
		ids = append(ids, id)
	}
	rows, err := tx.Query(ctx, `SELECT id,account_id,model_usage FROM v3_commerce.subscriptions
	 WHERE account_id=ANY($1) ORDER BY id FOR UPDATE`, ids)
	if err != nil {
		return nil, err
	}
	var updates []subscriptionUsageUpdate
	for rows.Next() {
		var u subscriptionUsageUpdate
		var account int64
		if err = rows.Scan(&u.id, &account, &u.usage); err != nil {
			rows.Close()
			return nil, err
		}
		if u.usage == nil {
			u.usage = map[string]int64{}
		}
		for model, amount := range changes[account] {
			if u.usage[model] < 0 {
				rows.Close()
				return nil, fmt.Errorf("ledger: negative subscription model usage")
			}
			next, addErr := credits.Micro(u.usage[model]).Add(amount)
			if addErr != nil {
				rows.Close()
				return nil, fmt.Errorf("ledger: subscription model usage overflow: %w", addErr)
			}
			u.usage[model] = int64(next)
		}
		updates = append(updates, u)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return updates, nil
}
