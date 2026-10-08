package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

// Poster commits business money movements and the Redis delivery outbox.
// PostTx lets a commerce state transition share the exact ledger transaction.
type Poster struct {
	pool *pgxpool.Pool
	rdb  *redisx.Client
	now  func() time.Time
}

// Pass Redis whenever accounts are shared with gateway traffic. PG-only mode
// is intended for offline imports and isolated databases without a gateway.
func NewPoster(pool *pgxpool.Pool, clients ...*redisx.Client) *Poster {
	p := &Poster{pool: pool, now: time.Now}
	if len(clients) > 0 {
		p.rdb = clients[0]
	}
	return p
}

func (p *Poster) Post(ctx context.Context, e billing.Entry) (billing.PostResult, error) {
	var result billing.PostResult
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		var err error
		result, err = p.PostTx(ctx, tx, e)
		return err
	})
	return result, err
}

func (p *Poster) PostTx(ctx context.Context, tx pgx.Tx, e billing.Entry) (billing.PostResult, error) {
	var result billing.PostResult
	if e.AccountID <= 0 || e.Amount == 0 || e.OperationID == "" || e.Kind == "" {
		return result, errors.New("ledger: entry requires account, nonzero amount, kind and operation ID")
	}
	meta := e.Metadata
	if meta == nil {
		meta = map[string]any{}
	}
	payload, err := json.Marshal(meta)
	if err != nil {
		return result, fmt.Errorf("ledger: metadata: %w", err)
	}
	result, replayed, err := p.replayIdempotentPost(ctx, tx, e, payload)
	if err != nil || replayed {
		return result, err
	}
	current, err := loadAccountForUpdateTx(ctx, tx, e.AccountID, &result)
	if err != nil {
		return result, err
	}
	if err = applyPostedBalance(e, current, &result); err != nil {
		return result, err
	}
	if err = ensureWalletTransferTx(ctx, tx, e, p.now()); err != nil {
		return result, err
	}
	reservation, err := p.reservePostingIfNeeded(ctx, tx, e, current, result.Version-1)
	if err != nil {
		return result, err
	}
	if err = insertLedgerEntryTx(ctx, tx, e, payload, &result); err != nil {
		return result, err
	}
	if err = recordFundingEntryTx(ctx, tx, e, current, p.now()); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, `UPDATE v3_billing.accounts SET balance = $2, version = $3 WHERE id = $1`,
		e.AccountID, int64(result.Balance), result.Version); err != nil {
		return result, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO v3_billing.balance_outbox (account_id, amount, version, operation_id, reservation_id) VALUES ($1,$2,$3,$4,NULLIF($5,''))`,
		e.AccountID, int64(e.Amount), result.Version, e.OperationID, reservation)
	return result, err
}

// replayIdempotentPost locks the operation ID and checks for a prior post. If
// one exists, replayed is true and result/err are PostTx's final return
// values (a duplicate result or ErrPostConflict); the caller must not continue.
func (p *Poster) replayIdempotentPost(ctx context.Context, tx pgx.Tx, e billing.Entry, payload []byte) (result billing.PostResult, replayed bool, err error) {
	// One operation across accounts is serialized before checking idempotency.
	// Hash collisions only serialize unrelated operations; they cannot alias them.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, e.OperationID); err != nil {
		return result, false, err
	}
	var identical bool
	err = tx.QueryRow(ctx, `SELECT e.id, a.balance, a.version,
	    e.account_id = $2 AND e.amount = $3 AND e.kind = $4 AND coalesce(e.request_id, '') = $5 AND e.reason = $6 AND e.metadata = $7::jsonb
	    FROM v3_billing.ledger_entries e JOIN v3_billing.accounts a ON a.id=e.account_id WHERE e.operation_id = $1`,
		e.OperationID, e.AccountID, int64(e.Amount), e.Kind, e.RequestID, e.Reason, payload).
		Scan(&result.EntryID, &result.Balance, &result.Version, &identical)
	if err == nil {
		if !identical {
			return result, true, billing.ErrPostConflict
		}
		result.Duplicate = true
		return result, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, true, err
	}
	return result, false, nil
}

// loadAccountForUpdateTx locks and reads the account's current balance,
// storing its version in result.
func loadAccountForUpdateTx(ctx context.Context, tx pgx.Tx, accountID int64, result *billing.PostResult) (credits.Micro, error) {
	var current int64
	err := tx.QueryRow(ctx, `SELECT balance, version FROM v3_billing.accounts WHERE id = $1 FOR UPDATE`, accountID).
		Scan(&current, &result.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrUnknownAccount
	}
	if err != nil {
		return 0, err
	}
	return credits.Micro(current), nil
}

// applyPostedBalance computes the post-entry balance and version into result
// and rejects the post if it would overdraw a non-adjustment account.
func applyPostedBalance(e billing.Entry, current credits.Micro, result *billing.PostResult) error {
	var err error
	result.Balance, err = current.Add(e.Amount)
	if err != nil {
		return err
	}
	if result.Version == math.MaxInt64 {
		return errors.New("ledger: account version overflow")
	}
	result.Version++
	if e.Amount < 0 && result.Balance < 0 && e.Kind != "adjustment" && e.Kind != "subscription_expire" {
		return gateway.ErrInsufficientCredits
	}
	return nil
}

// reservePostingIfNeeded records the Redis delivery reservation for debits
// that must notify the gateway, when a Redis client is configured.
func (p *Poster) reservePostingIfNeeded(ctx context.Context, tx pgx.Tx, e billing.Entry, current credits.Micro, priorVersion int64) (string, error) {
	if p.rdb == nil || e.Amount >= 0 || e.Kind == "adjustment" {
		return "", nil
	}
	if e.Amount == credits.Micro(math.MinInt64) {
		return "", credits.ErrOverflow
	}
	var transactionID string
	if err := tx.QueryRow(ctx, `SELECT pg_current_xact_id()::text`).Scan(&transactionID); err != nil {
		return "", err
	}
	return billing.ReservePosting(ctx, p.rdb, e.AccountID, e.OperationID, transactionID, -e.Amount, current, priorVersion, p.now())
}

// insertLedgerEntryTx writes the ledger_entries row for e and stores the new
// entry ID in result.
func insertLedgerEntryTx(ctx context.Context, tx pgx.Tx, e billing.Entry, payload []byte, result *billing.PostResult) error {
	return tx.QueryRow(ctx, `INSERT INTO v3_billing.ledger_entries
	    (account_id, amount, balance_after, kind, operation_id, request_id, reason, metadata)
	    VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),$7,$8) RETURNING id`,
		e.AccountID, int64(e.Amount), int64(result.Balance), e.Kind, e.OperationID, e.RequestID, e.Reason, payload).
		Scan(&result.EntryID)
}
