// Package ledger is the PostgreSQL side of billing: account lookup and
// balance reads for the Redis settler. Consuming billing events into ledger
// rows (plan §5) is the next step and lives here too.
package ledger

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// Accounts implements billing.AccountResolver and billing.BalanceLoader.
//
// Wallet account ids never change once created, so they are cached for the
// life of the process: PostgreSQL is hit once per user per gateway process,
// not per request.
type Accounts struct {
	pool    *pgxpool.Pool
	wallets sync.Map // user id -> account id
}

// NewAccounts returns an Accounts reader.
func NewAccounts(pool *pgxpool.Pool) *Accounts { return &Accounts{pool: pool} }

// WalletAccount returns the user's wallet account, creating it on first use.
func (a *Accounts) WalletAccount(ctx context.Context, userID int64) (int64, error) {
	if id, ok := a.wallets.Load(userID); ok {
		return id.(int64), nil
	}
	var id int64
	err := a.pool.QueryRow(ctx, `
		INSERT INTO v3_billing.accounts (owner_type, owner_id, kind)
		VALUES ('user', $1, 'wallet')
		ON CONFLICT (owner_type, owner_id, kind) DO UPDATE SET owner_id = EXCLUDED.owner_id
		RETURNING id`, userID).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("ledger: wallet account for user %d: %w", userID, err)
	}
	a.wallets.Store(userID, id)
	return id, nil
}

// WarmWallets loads known wallet IDs before the gateway accepts traffic.
func (a *Accounts) WarmWallets(ctx context.Context) error {
	rows, err := a.pool.Query(ctx, `SELECT owner_id, id FROM v3_billing.accounts WHERE owner_type = 'user' AND kind = 'wallet'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var user, id int64
		if err := rows.Scan(&user, &id); err != nil {
			return err
		}
		a.wallets.Store(user, id)
	}
	return rows.Err()
}

// ErrUnknownAccount is returned for an account id with no row.
var ErrUnknownAccount = errors.New("ledger: unknown account")

// LedgerBalance returns an account's balance and version as last posted by
// the ledger worker.
func (a *Accounts) LedgerBalance(ctx context.Context, accountID int64) (credits.Micro, int64, error) {
	var balance, version int64
	err := a.pool.QueryRow(ctx, `SELECT balance, version FROM v3_billing.accounts WHERE id = $1`, accountID).Scan(&balance, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, fmt.Errorf("%w: %d", ErrUnknownAccount, accountID)
	}
	if err != nil {
		return 0, 0, fmt.Errorf("ledger: balance of account %d: %w", accountID, err)
	}
	return credits.Micro(balance), version, nil
}
