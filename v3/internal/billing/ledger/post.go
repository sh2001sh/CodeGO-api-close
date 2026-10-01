package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// postResult says what happened to each event of a batch.
type postResult struct {
	posted, duplicates, conflicts, deadLetters, skipped int
}

// dedupKey identifies a charge: one request can charge one account once.
type dedupKey struct {
	requestID string
	accountID int64
}

// Marketplace progress precedes subscription state, which precedes income
// hooks and account balances. Blind-box subscription grants use that same
// ordering, preventing a ledger worker and an opening from locking inversely.
func postWithMarketplace(ctx context.Context, pool *pgxpool.Pool, batch []event, bad []deadLetter, recorder UsageRecorder, hooks ...UsageHook) (postResult, error) {
	var res postResult
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		fresh, err := claimFreshCharges(ctx, tx, batch, &res, &bad)
		if err != nil {
			return err
		}
		if err := recordMarketplaceAndSubscriptionUsage(ctx, tx, recorder, fresh); err != nil {
			return err
		}
		// Domain state/progress locks precede account locks, matching commerce
		// and marketplace business transactions. Dedup makes callbacks replay safe.
		if err := runUsageHooks(ctx, tx, fresh, hooks); err != nil {
			return err
		}
		debits := make([]event, 0, len(fresh))
		for _, e := range fresh {
			if e.amount != 0 {
				debits = append(debits, e)
			}
		}
		if err := applyCharges(ctx, tx, debits); err != nil {
			return err
		}
		if err := recordRequestEconomicsTx(ctx, tx, fresh); err != nil {
			return err
		}
		if err := insertUsageLogs(ctx, tx, fresh); err != nil {
			return err
		}
		res.posted = len(fresh)
		res.deadLetters = len(bad)
		return insertDeadLetters(ctx, tx, bad)
	})
	return res, err
}

// claimFreshCharges filters batch down to events that move money or carry
// model usage, claims them against the dedup table (recording skip count
// into res), and drops events for accounts that no longer exist (recording
// them into bad).
func claimFreshCharges(ctx context.Context, tx pgx.Tx, batch []event, res *postResult, bad *[]deadLetter) ([]event, error) {
	charges := make([]event, 0, len(batch))
	for _, e := range batch {
		if e.amount == 0 && e.fields[billing.FieldModel] == "" {
			res.skipped++ // releases and sweeps move no money
			continue
		}
		charges = append(charges, e)
	}
	fresh, err := claimFresh(ctx, tx, charges, res)
	if err != nil {
		return nil, err
	}
	return dropUnknownAccounts(ctx, tx, fresh, bad)
}

// recordMarketplaceAndSubscriptionUsage records marketplace model usage for
// each primary-funding event with a model, then applies the batch's
// subscription model usage deltas.
func recordMarketplaceAndSubscriptionUsage(ctx context.Context, tx pgx.Tx, recorder UsageRecorder, fresh []event) error {
	for _, e := range fresh {
		if e.fields[billing.FieldModel] == "" || e.fields["funding_part"] == "secondary" {
			continue
		}
		if err := recordMarketplaceUsage(ctx, tx, recorder, e.fields); err != nil {
			return err
		}
	}
	return recordSubscriptionModelUsageTx(ctx, tx, fresh)
}

// runUsageHooks invokes every hook for each primary-funding event with a
// model.
func runUsageHooks(ctx context.Context, tx pgx.Tx, fresh []event, hooks []UsageHook) error {
	for _, e := range fresh {
		if e.fields[billing.FieldModel] == "" || e.fields["funding_part"] == "secondary" {
			continue
		}
		for _, hook := range hooks {
			if hook != nil {
				if err := hook(ctx, tx, e.fields); err != nil {
					return fmt.Errorf("ledger: usage callback %s: %w", e.requestID, err)
				}
			}
		}
	}
	return nil
}

// claimFresh inserts dedup rows and returns the events seen for the first
// time. Repeats with the same fingerprint are duplicates (redelivery); repeats
// with a different fingerprint are conflicts parked for review.
func claimFresh(ctx context.Context, tx pgx.Tx, charges []event, res *postResult) ([]event, error) {
	if len(charges) == 0 {
		return nil, nil
	}
	reqs, accts, fps := make([]string, len(charges)), make([]int64, len(charges)), make([][]byte, len(charges))
	for i, e := range charges {
		reqs[i], accts[i], fps[i] = e.requestID, e.accountID, e.fingerprint[:]
	}
	rows, err := tx.Query(ctx, `
		INSERT INTO v3_billing.billing_dedup (request_id, account_id, fingerprint)
		SELECT * FROM unnest($1::text[], $2::bigint[], $3::bytea[])
		ON CONFLICT (request_id, account_id) DO NOTHING
		RETURNING request_id, account_id`, reqs, accts, fps)
	if err != nil {
		return nil, fmt.Errorf("ledger: insert dedup: %w", err)
	}
	inserted := make(map[dedupKey]bool, len(charges))
	for rows.Next() {
		var k dedupKey
		if err := rows.Scan(&k.requestID, &k.accountID); err != nil {
			return nil, err
		}
		inserted[k] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ledger: insert dedup: %w", err)
	}

	var fresh, repeats []event
	for _, e := range charges {
		k := dedupKey{e.requestID, e.accountID}
		if inserted[k] {
			fresh = append(fresh, e)
			delete(inserted, k) // a key repeated within one batch is a repeat
		} else {
			repeats = append(repeats, e)
		}
	}
	return fresh, classifyRepeats(ctx, tx, repeats, res)
}

func classifyRepeats(ctx context.Context, tx pgx.Tx, repeats []event, res *postResult) error {
	for _, e := range repeats {
		var existing []byte
		if err := tx.QueryRow(ctx, `SELECT fingerprint FROM v3_billing.billing_dedup WHERE request_id = $1 AND account_id = $2`,
			e.requestID, e.accountID).Scan(&existing); err != nil {
			return fmt.Errorf("ledger: read dedup %s: %w", e.requestID, err)
		}
		if string(existing) == string(e.fingerprint[:]) {
			res.duplicates++
			continue
		}
		payload, _ := json.Marshal(e.fields)
		if _, err := tx.Exec(ctx, `
			INSERT INTO v3_billing.dedup_conflicts (request_id, account_id, existing_fingerprint, incoming_fingerprint, payload)
			VALUES ($1, $2, $3, $4, $5)`, e.requestID, e.accountID, existing, e.fingerprint[:], payload); err != nil {
			return fmt.Errorf("ledger: record conflict %s: %w", e.requestID, err)
		}
		res.conflicts++
	}
	return nil
}

// applyCharges locks the touched accounts in id order (no deadlocks between
// concurrent workers), writes one ledger entry per charge with a running
// balance, then stores each account's final balance.
func applyCharges(ctx context.Context, tx pgx.Tx, charges []event) error {
	if len(charges) == 0 {
		return nil
	}
	balances, err := lockAccounts(ctx, tx, accountIDs(charges))
	if err != nil {
		return err
	}
	versions := make(map[int64]int64, len(balances))
	entries := make([][]any, 0, len(charges))
	for _, e := range charges {
		if e.amount == 0 {
			continue
		}
		if err := recordFundingUsageTx(ctx, tx, e, credits.Micro(balances[e.accountID])); err != nil {
			return err
		}
		next, err := credits.Micro(balances[e.accountID]).Add(-credits.Micro(e.amount))
		if err != nil {
			return fmt.Errorf("ledger: account %d debit overflow: %w", e.accountID, err)
		}
		balances[e.accountID] = int64(next)
		versions[e.accountID]++
		meta, _ := json.Marshal(e.fields)
		entries = append(entries, []any{e.accountID, -e.amount, balances[e.accountID], "usage",
			fmt.Sprintf("usage:%d:%s", e.accountID, e.requestID), e.requestID, meta})
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"v3_billing", "ledger_entries"},
		[]string{"account_id", "amount", "balance_after", "kind", "operation_id", "request_id", "metadata"},
		pgx.CopyFromRows(entries)); err != nil {
		return fmt.Errorf("ledger: copy entries: %w", err)
	}
	ids, bals, vers := make([]int64, 0, len(balances)), make([]int64, 0, len(balances)), make([]int64, 0, len(balances))
	for id, b := range balances {
		ids, bals, vers = append(ids, id), append(bals, b), append(vers, versions[id])
	}
	_, err = tx.Exec(ctx, `
		UPDATE v3_billing.accounts a SET balance = u.balance, version = a.version + u.n
		FROM unnest($1::bigint[], $2::bigint[], $3::bigint[]) AS u(id, balance, n)
		WHERE a.id = u.id`, ids, bals, vers)
	if err != nil {
		return fmt.Errorf("ledger: update balances: %w", err)
	}
	return nil
}

func accountIDs(events []event) []int64 {
	seen := make(map[int64]bool)
	var ids []int64
	for _, e := range events {
		if !seen[e.accountID] {
			seen[e.accountID] = true
			ids = append(ids, e.accountID)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func lockAccounts(ctx context.Context, tx pgx.Tx, ids []int64) (map[int64]int64, error) {
	rows, err := tx.Query(ctx, `SELECT id, balance FROM v3_billing.accounts WHERE id = ANY($1) ORDER BY id FOR UPDATE`, ids)
	if err != nil {
		return nil, fmt.Errorf("ledger: lock accounts: %w", err)
	}
	defer rows.Close()
	balances := make(map[int64]int64, len(ids))
	for rows.Next() {
		var id, bal int64
		if err := rows.Scan(&id, &bal); err != nil {
			return nil, err
		}
		balances[id] = bal
	}
	return balances, rows.Err()
}
