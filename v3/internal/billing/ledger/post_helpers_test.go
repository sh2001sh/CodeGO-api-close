//go:build pgintegration

package ledger

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// post exercises batch posting without a marketplace recorder.
func post(ctx context.Context, pool *pgxpool.Pool, batch []event, bad []deadLetter, hooks ...UsageHook) (postResult, error) {
	return postWithMarketplace(ctx, pool, batch, bad, nil, hooks...)
}
