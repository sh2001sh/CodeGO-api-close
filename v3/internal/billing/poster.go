package billing

import (
	"context"
	"errors"

	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// Entry is one signed, immutable movement of money. OperationID identifies the
// business action and must survive callback retries. Positive amounts credit
// an account; negative amounts debit it. Usage comes from the Redis event worker.
type Entry struct {
	AccountID   int64
	Amount      credits.Micro
	Kind        string
	OperationID string
	RequestID   string
	Reason      string
	Metadata    map[string]any
}

// PostResult identifies the ledger entry, including an idempotent redelivery.
type PostResult struct {
	EntryID   int64
	Balance   credits.Micro
	Version   int64
	Duplicate bool
}

var ErrPostConflict = errors.New("billing: operation ID reused with different entry")

var ErrAPICreditsPurchaseLocked = errors.New("ledger: API-only credits cannot fund purchases")

// Poster is the only business-facing entry point for topups, redemptions,
// subscriptions, refunds and marketplace transfers. Implementations commit the
// ledger and a durable hot-balance update together.
type Poster interface {
	Post(context.Context, Entry) (PostResult, error)
}
