package catalog

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

// outboxBatchSize bounds how many rows one relay tick leases at a time.
const outboxBatchSize = 200

// outboxTick is how often a relay looks for new rows when idle.
const outboxTick = 500 * time.Millisecond

// OutboxRelay drains v3_platform.cache_invalidation_outbox: it leases rows
// with FOR UPDATE SKIP LOCKED so multiple relay instances never double
// deliver a row, publishes each as "entity:entity_id" on
// redisx.ChannelInvalidate, then deletes it. Rows for the 'catalog' or
// 'pricing' entities also trigger a (coalesced) snapshot publish.
type OutboxRelay struct {
	pool      *pgxpool.Pool
	redis     *redisx.Client
	publisher *Publisher
	log       *slog.Logger
}

// NewOutboxRelay builds a relay. publisher may be nil in tests that only
// care about invalidation delivery, not snapshot compilation.
func NewOutboxRelay(pool *pgxpool.Pool, redis *redisx.Client, publisher *Publisher, log *slog.Logger) *OutboxRelay {
	if log == nil {
		log = slog.Default()
	}
	return &OutboxRelay{pool: pool, redis: redis, publisher: publisher, log: log}
}

// Run drains the outbox on outboxTick until ctx is canceled.
func (r *OutboxRelay) Run(ctx context.Context) error {
	ticker := time.NewTicker(outboxTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			for {
				n, err := r.drainBatch(ctx)
				if err != nil {
					r.log.Error("catalog: outbox drain failed", "err", err)
					break
				}
				if n < outboxBatchSize {
					break // caught up; wait for the next tick
				}
			}
		}
	}
}

type outboxRow struct {
	id       int64
	entity   string
	entityID string
}

// drainBatch leases up to outboxBatchSize rows, publishes each, deletes the
// published ones and returns how many it processed.
func (r *OutboxRelay) drainBatch(ctx context.Context) (int, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("catalog: outbox begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		SELECT id, entity, entity_id
		FROM v3_platform.cache_invalidation_outbox
		WHERE leased_until IS NULL OR leased_until < now()
		ORDER BY id
		LIMIT $1
		FOR UPDATE SKIP LOCKED`, outboxBatchSize)
	if err != nil {
		return 0, fmt.Errorf("catalog: outbox select: %w", err)
	}
	batch, scanErr := collectOutboxRows(rows)
	if scanErr != nil {
		return 0, scanErr
	}
	if len(batch) == 0 {
		return 0, nil
	}

	ids := make([]int64, len(batch))
	needsSnapshot := false
	for i, row := range batch {
		ids[i] = row.id
		if row.entity == "catalog" || row.entity == "pricing" || row.entity == "settings" || row.entity == "account_profile" || row.entity == "user" || row.entity == "api_key" {
			needsSnapshot = true
		}
		if err := r.redis.Publish(ctx, redisx.ChannelInvalidate, row.entity+":"+row.entityID).Err(); err != nil {
			return 0, fmt.Errorf("catalog: publish invalidation %d: %w", row.id, err)
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM v3_platform.cache_invalidation_outbox WHERE id = ANY($1)`, ids); err != nil {
		return 0, fmt.Errorf("catalog: outbox delete: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("catalog: outbox commit: %w", err)
	}

	if needsSnapshot && r.publisher != nil {
		r.publisher.Publish(ctx)
	}
	return len(batch), nil
}

func collectOutboxRows(rows pgx.Rows) ([]outboxRow, error) {
	defer rows.Close()
	var batch []outboxRow
	for rows.Next() {
		var row outboxRow
		if err := rows.Scan(&row.id, &row.entity, &row.entityID); err != nil {
			return nil, fmt.Errorf("catalog: outbox scan: %w", err)
		}
		batch = append(batch, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return batch, nil
}
