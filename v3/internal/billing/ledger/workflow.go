package ledger

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// AsyncTaskExists is read by the reservation sweeper, never by the gateway
// request path. An ownership mismatch cannot authorize another task's refund.
func (a *Accounts) AsyncTaskExists(ctx context.Context, requestID string, userID, keyID int64) (bool, error) {
	var owner, key int64
	err := a.pool.QueryRow(ctx, `SELECT user_id,key_id FROM v3_workflow.tasks WHERE id=$1`, requestID).Scan(&owner, &key)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("ledger: lookup async task: %w", err)
	}
	if owner != userID || key != keyID {
		return false, errors.New("ledger: async task ownership mismatch")
	}
	return true, nil
}
