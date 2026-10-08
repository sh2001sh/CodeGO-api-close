package ledger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

// WorkerConfig tunes the ledger worker. Zero values select defaults.
type WorkerConfig struct {
	Consumer       string        // unique per process, e.g. hostname-pid (required)
	Batch          int64         // entries per transaction, default 500
	Block          time.Duration // XREADGROUP block, default 1 s
	ClaimIdle      time.Duration // reclaim entries a dead consumer left unacked this long, default 60 s
	TrimEvery      time.Duration // default 30 s
	Marketplace    UsageRecorder
	UsageHook      UsageHook
	UsageBatchHook UsageBatchHook
}

func (c WorkerConfig) withDefaults() WorkerConfig {
	if c.Batch <= 0 {
		c.Batch = 500
	}
	if c.Block <= 0 {
		c.Block = time.Second
	}
	if c.ClaimIdle <= 0 {
		c.ClaimIdle = time.Minute
	}
	if c.TrimEvery <= 0 {
		c.TrimEvery = 30 * time.Second
	}
	return c
}

// Worker turns billing events into ledger rows. Delivery is at least once:
// entries are acknowledged only after their transaction commits, and the
// dedup table makes redelivery harmless. Any number of workers may share the
// consumer group.
type Worker struct {
	cfg  WorkerConfig
	pool *pgxpool.Pool
	rdb  *redisx.Client
	log  *slog.Logger
}

// NewWorker returns a ledger worker.
func NewWorker(pool *pgxpool.Pool, rdb *redisx.Client, cfg WorkerConfig, log *slog.Logger) (*Worker, error) {
	if cfg.Consumer == "" {
		return nil, errors.New("ledger: worker needs a unique consumer name")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Worker{cfg: cfg.withDefaults(), pool: pool, rdb: rdb, log: log}, nil
}

// Run consumes until ctx is canceled.
func (w *Worker) Run(ctx context.Context) error {
	if err := w.ensureGroup(ctx); err != nil {
		return err
	}
	lastTrim := time.Now()
	for ctx.Err() == nil {
		if _, err := w.Step(ctx); err != nil && ctx.Err() == nil {
			w.log.Error("ledger: step failed; retrying", "err", err)
			sleepCtx(ctx, time.Second)
		}
		if time.Since(lastTrim) >= w.cfg.TrimEvery {
			if err := w.Trim(ctx); err != nil {
				w.log.Error("ledger: trim failed", "err", err)
			}
			lastTrim = time.Now()
		}
	}
	return nil
}

func (w *Worker) ensureGroup(ctx context.Context) error {
	err := w.rdb.XGroupCreateMkStream(ctx, redisx.StreamBillingEvents, redisx.GroupLedger, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("ledger: create consumer group: %w", err)
	}
	return nil
}

// Step posts one batch: first entries reclaimed from dead consumers, else new
// ones. It returns how many entries it acknowledged.
func (w *Worker) Step(ctx context.Context) (int, error) {
	msgs, err := w.reclaim(ctx)
	if err != nil {
		return 0, err
	}
	if len(msgs) == 0 {
		if msgs, err = w.read(ctx); err != nil {
			return 0, err
		}
	}
	if len(msgs) == 0 {
		return 0, nil
	}
	return w.process(ctx, msgs)
}

func (w *Worker) read(ctx context.Context) ([]redis.XMessage, error) {
	streams, err := w.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: redisx.GroupLedger, Consumer: w.cfg.Consumer,
		Streams: []string{redisx.StreamBillingEvents, ">"}, Count: w.cfg.Batch, Block: w.cfg.Block,
	}).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: read events: %w", err)
	}
	return streams[0].Messages, nil
}

func (w *Worker) reclaim(ctx context.Context) ([]redis.XMessage, error) {
	msgs, _, err := w.rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream: redisx.StreamBillingEvents, Group: redisx.GroupLedger, Consumer: w.cfg.Consumer,
		MinIdle: w.cfg.ClaimIdle, Start: "0", Count: w.cfg.Batch,
	}).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("ledger: reclaim events: %w", err)
	}
	return msgs, nil
}

func (w *Worker) process(ctx context.Context, msgs []redis.XMessage) (int, error) {
	batch := make([]event, 0, len(msgs))
	var bad []deadLetter
	ids := make([]string, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
		e, err := parseEvent(m.ID, m.Values)
		if err != nil {
			bad = append(bad, deadLetter{streamID: m.ID, reason: err.Error(), fields: e.fields})
			continue
		}
		batch = append(batch, e)
	}
	res, err := postWithMarketplaceBatch(ctx, w.pool, batch, bad, w.cfg.Marketplace, w.cfg.UsageBatchHook, w.cfg.UsageHook)
	if err != nil {
		return 0, err // not acknowledged: redelivered and deduplicated later
	}
	if err := w.rdb.XAck(ctx, redisx.StreamBillingEvents, redisx.GroupLedger, ids...).Err(); err != nil {
		return 0, fmt.Errorf("ledger: ack %d events: %w", len(ids), err)
	}
	if res.conflicts > 0 || res.deadLetters > 0 {
		w.log.Error("ledger: events need review", "conflicts", res.conflicts, "dead_letters", res.deadLetters)
	}
	return len(ids), nil
}

// Backlog reports how far the ledger group is behind: entries delivered but
// not acknowledged, and entries not delivered yet.
func (w *Worker) Backlog(ctx context.Context) (pending, undelivered int64, err error) {
	groups, err := w.rdb.XInfoGroups(ctx, redisx.StreamBillingEvents).Result()
	if err != nil {
		return 0, 0, fmt.Errorf("ledger: backlog: %w", err)
	}
	for _, g := range groups {
		if g.Name == redisx.GroupLedger {
			return g.Pending, g.Lag, nil
		}
	}
	return 0, 0, nil
}

func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
