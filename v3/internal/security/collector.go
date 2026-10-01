package security

import (
	"context"
	"sort"

	"github.com/jackc/pgx/v5"
)

type queuedSample struct {
	request                string
	user, channel          int64
	model                  string
	input, cache, observed int64
}

// processUsage runs separately from money posting. The entire bounded sample
// batch and its restriction episodes commit together; a retry cannot recount
// the same queue row, and financial replays never enqueue a fresh sample.
func (g *Guard) processUsage(ctx context.Context, limit int) error {
	return pgx.BeginFunc(ctx, g.pool, func(tx pgx.Tx) error {
		// A single collector avoids processing newer minutes before an older batch
		// held by another collector. Contention returns immediately to the worker.
		var acquired bool
		if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended('v3:security:usage-collector',0))`).Scan(&acquired); err != nil {
			return err
		}
		if !acquired {
			return nil
		}
		samples, err := dequeueUsageSamplesTx(ctx, tx, limit)
		if err != nil {
			return err
		}
		if err := lockSampleUsersInOrderTx(ctx, tx, samples); err != nil {
			return err
		}
		return g.applyUsageSamplesTx(ctx, tx, samples)
	})
}

// dequeueUsageSamplesTx claims up to limit queued usage samples, oldest
// first, skipping rows another collector already holds.
func dequeueUsageSamplesTx(ctx context.Context, tx pgx.Tx, limit int) ([]queuedSample, error) {
	rows, err := tx.Query(ctx, `SELECT request_id,user_id,channel_id,model,input_tokens,cached_tokens,observed_at FROM v3_security.usage_queue ORDER BY observed_at,request_id LIMIT $1 FOR UPDATE SKIP LOCKED`, limit)
	if err != nil {
		return nil, err
	}
	var samples []queuedSample
	for rows.Next() {
		var s queuedSample
		if err := rows.Scan(&s.request, &s.user, &s.channel, &s.model, &s.input, &s.cache, &s.observed); err != nil {
			rows.Close()
			return nil, err
		}
		samples = append(samples, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return samples, nil
}

// lockSampleUsersInOrderTx locks every distinct user referenced in samples,
// ascending by ID. A bounded batch may span multiple accounts; acquiring
// identity rows in ascending order (matching wallet transfers) ensures
// observation order itself never becomes a lock order that deadlocks with a
// peer transfer.
func lockSampleUsersInOrderTx(ctx context.Context, tx pgx.Tx, samples []queuedSample) error {
	users := make([]int64, 0, len(samples))
	for _, sample := range samples {
		users = append(users, sample.user)
	}
	sort.Slice(users, func(i, j int) bool { return users[i] < users[j] })
	var previous int64
	for _, user := range users {
		if user == previous {
			continue
		}
		if _, err := tx.Exec(ctx, `SELECT id FROM v3_identity.users WHERE id=$1 FOR UPDATE`, user); err != nil {
			return err
		}
		previous = user
	}
	return nil
}

// applyUsageSamplesTx records each sample observed within the live window
// and removes it from the queue regardless of whether it was recent enough
// to record.
func (g *Guard) applyUsageSamplesTx(ctx context.Context, tx pgx.Tx, samples []queuedSample) error {
	now := g.cfg.Now().Unix()
	for _, s := range samples {
		// Historical backlogs cannot prove the current live traffic pattern.
		if s.observed >= now-300 && s.observed <= now {
			if err := g.recordSampleTx(ctx, tx, s.user, s.channel, s.model, s.input, s.cache, s.observed); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM v3_security.usage_queue WHERE request_id=$1`, s.request); err != nil {
			return err
		}
	}
	return nil
}
