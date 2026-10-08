package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
)

// PostAccrualsTx batches held supplier earnings and platform commissions only.
// Wallet credits, funding provenance and debit reservations continue through
// PostTx. Each fresh credit retains its own ledger row, version and outbox row.
// debitAccounts participates only in the global lock set; its balances are
// untouched here and later usage debits reuse these transaction-held locks.
func (p *Poster) PostAccrualsTx(ctx context.Context, tx pgx.Tx, entries []billing.Entry, debitAccounts []int64) error {
	if len(entries) == 0 {
		return nil
	}
	operations := make([]string, len(entries))
	accounts, amounts := make([]int64, len(entries)), make([]int64, len(entries))
	kinds, requests, reasons, payloads := make([]string, len(entries)), make([]string, len(entries)), make([]string, len(entries)), make([]string, len(entries))
	for i, e := range entries {
		if e.AccountID <= 0 || e.Amount <= 0 || e.Kind != "marketplace_accrue" || e.OperationID == "" {
			return errors.New("ledger: accrual batch requires a positive marketplace credit")
		}
		meta := e.Metadata
		if meta == nil {
			meta = map[string]any{}
		}
		payload, err := json.Marshal(meta)
		if err != nil {
			return fmt.Errorf("ledger: metadata: %w", err)
		}
		operations[i], accounts[i], amounts[i] = e.OperationID, e.AccountID, int64(e.Amount)
		kinds[i], requests[i], reasons[i], payloads[i] = e.Kind, e.RequestID, e.Reason, string(payload)
	}
	// Sort the advisory lock keys themselves, including hash collisions. This
	// matches PostTx's operation lock and prevents inverse batch lock ordering.
	rows, err := tx.Query(ctx, `SELECT pg_advisory_xact_lock(key) FROM
	 (SELECT DISTINCT hashtextextended(op,0) AS key FROM unnest($1::text[]) op ORDER BY key) locks`, operations)
	if err != nil {
		return err
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	rows, err = tx.Query(ctx, `WITH input AS (
	 SELECT * FROM unnest($1::text[],$2::bigint[],$3::bigint[],$4::text[],$5::text[],$6::text[],$7::text[])
	 WITH ORDINALITY AS i(operation_id,account_id,amount,kind,request_id,reason,metadata,ordinal)),
	 first AS (SELECT DISTINCT ON(operation_id) * FROM input ORDER BY operation_id,ordinal)
	 SELECT i.ordinal,e.id IS NOT NULL,
	 CASE WHEN e.id IS NULL THEN true ELSE e.account_id=i.account_id AND e.amount=i.amount
	 AND e.kind=i.kind AND coalesce(e.request_id,'')=i.request_id AND e.reason=i.reason
	 AND e.metadata=i.metadata::jsonb END,
	 f.account_id=i.account_id AND f.amount=i.amount AND f.kind=i.kind
	 AND f.request_id=i.request_id AND f.reason=i.reason AND f.metadata::jsonb=i.metadata::jsonb,
	 f.ordinal<i.ordinal
	 FROM input i JOIN first f USING(operation_id)
	 LEFT JOIN v3_billing.ledger_entries e USING(operation_id) ORDER BY i.ordinal`,
		operations, accounts, amounts, kinds, requests, reasons, payloads)
	if err != nil {
		return err
	}
	fresh := make([]int, 0, len(entries))
	for rows.Next() {
		var ordinal int
		var exists, same, repeatedSame, repeated bool
		if err = rows.Scan(&ordinal, &exists, &same, &repeatedSame, &repeated); err != nil {
			rows.Close()
			return err
		}
		if !same || !repeatedSame {
			rows.Close()
			return billing.ErrPostConflict
		}
		if !exists && !repeated {
			fresh = append(fresh, ordinal-1)
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil || len(fresh) == 0 {
		return err
	}
	touched := make(map[int64]struct{}, len(fresh))
	for _, i := range fresh {
		touched[accounts[i]] = struct{}{}
	}
	creditIDs := make([]int64, 0, len(touched))
	for id := range touched {
		creditIDs = append(creditIDs, id)
	}
	allAccounts := make(map[int64]struct{}, len(touched)+len(debitAccounts))
	for id := range touched {
		allAccounts[id] = struct{}{}
	}
	for _, id := range debitAccounts {
		allAccounts[id] = struct{}{}
	}
	ids := make([]int64, 0, len(allAccounts))
	for id := range allAccounts {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	rows, err = tx.Query(ctx, `SELECT id,balance,version,kind FROM v3_billing.accounts WHERE id=ANY($1) ORDER BY id FOR UPDATE`, ids)
	if err != nil {
		return err
	}
	states := make(map[int64]billing.PostResult, len(ids))
	for rows.Next() {
		var id int64
		var state billing.PostResult
		var kind string
		if err = rows.Scan(&id, &state.Balance, &state.Version, &kind); err != nil {
			rows.Close()
			return err
		}
		_, credited := touched[id]
		if credited && kind != "marketplace_pending" && kind != "platform_revenue" {
			rows.Close()
			return errors.New("ledger: accrual batch cannot credit wallet or spending accounts")
		}
		states[id] = state
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	if len(states) != len(ids) {
		return ErrUnknownAccount
	}
	ledgerRows, outboxRows := make([][]any, 0, len(fresh)), make([][]any, 0, len(fresh))
	for _, i := range fresh {
		e := entries[i]
		state := states[e.AccountID]
		if err = applyPostedBalance(e, state.Balance, &state); err != nil {
			return err
		}
		states[e.AccountID] = state
		var request any
		if e.RequestID != "" {
			request = e.RequestID
		}
		ledgerRows = append(ledgerRows, []any{e.AccountID, int64(e.Amount), int64(state.Balance), e.Kind, e.OperationID, request, e.Reason, json.RawMessage(payloads[i])})
		outboxRows = append(outboxRows, []any{e.AccountID, int64(e.Amount), state.Version, e.OperationID})
	}
	if _, err = tx.CopyFrom(ctx, pgx.Identifier{"v3_billing", "ledger_entries"}, []string{"account_id", "amount", "balance_after", "kind", "operation_id", "request_id", "reason", "metadata"}, pgx.CopyFromRows(ledgerRows)); err != nil {
		return err
	}
	balances, versions := make([]int64, len(creditIDs)), make([]int64, len(creditIDs))
	for i, id := range creditIDs {
		balances[i], versions[i] = int64(states[id].Balance), states[id].Version
	}
	if _, err = tx.Exec(ctx, `UPDATE v3_billing.accounts a SET balance=i.balance,version=i.version
	 FROM unnest($1::bigint[],$2::bigint[],$3::bigint[]) i(id,balance,version) WHERE a.id=i.id`, creditIDs, balances, versions); err != nil {
		return err
	}
	_, err = tx.CopyFrom(ctx, pgx.Identifier{"v3_billing", "balance_outbox"}, []string{"account_id", "amount", "version", "operation_id"}, pgx.CopyFromRows(outboxRows))
	return err
}
