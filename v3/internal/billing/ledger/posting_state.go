package ledger

import (
	"context"
	"fmt"

	"github.com/sh2001sh/new-api/v3/internal/billing"
)

// PostingState checks durable transaction facts, never a process-local status.
// A committed transaction without the operation's entry spent no money and
// releases the hold, just as an aborted transaction does.
func (a *Accounts) PostingState(ctx context.Context, operationID, transactionID string) (billing.PostingState, error) {
	var state *string
	if err := a.pool.QueryRow(ctx, `SELECT pg_xact_status($1::xid8)::text`, transactionID).Scan(&state); err != nil {
		return billing.PostingInProgress, err
	}
	if state == nil {
		return billing.PostingInProgress, fmt.Errorf("ledger: transaction %s status is unavailable; hold retained", transactionID)
	}
	switch *state {
	case "in progress":
		return billing.PostingInProgress, nil
	case "aborted":
		return billing.PostingAborted, nil
	case "committed":
		// Read the entry AFTER observing commit. Reversing this order could
		// see no entry immediately before commit, then release a live debit.
		var posted bool
		if err := a.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_billing.ledger_entries WHERE operation_id=$1)`, operationID).Scan(&posted); err != nil {
			return billing.PostingInProgress, err
		}
		if posted {
			return billing.PostingCommitted, nil
		}
		return billing.PostingAborted, nil
	default:
		return billing.PostingInProgress, fmt.Errorf("ledger: unknown transaction status %q", *state)
	}
}
