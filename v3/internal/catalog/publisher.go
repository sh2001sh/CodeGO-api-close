package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

// coalesceWindow batches invalidations that arrive close together into one
// Compile+publish, so a burst of catalog writes produces one new version.
const coalesceWindow = 200 * time.Millisecond

// snapshotTTL is how long a superseded snapshot blob stays in Redis, so a
// gateway that saw the old announcement late can still load it.
const snapshotTTL = time.Hour

// Publisher compiles snapshots and makes them visible to gateways: it bumps
// v3_platform.snapshot_versions, writes the blob to Redis and publishes the
// new version on ChannelSnapshot. Concurrent Publish calls within
// coalesceWindow collapse into a single compile+publish.
type Publisher struct {
	pool  *pgxpool.Pool
	redis *redisx.Client
	dec   Decrypter
	enc   Encrypter
	log   *slog.Logger

	mu      sync.Mutex
	pending bool
	timer   *time.Timer
}

// NewPublisher builds a Publisher. enc re-encrypts credential secrets before
// they are written to Redis: the snapshot blob carries the same ciphertext
// protection as the database, and dec is used symmetrically by Store readers
// that need plaintext (the gateway's routing layer decrypts on demand, not
// this package).
func NewPublisher(pool *pgxpool.Pool, redis *redisx.Client, dec Decrypter, enc Encrypter, log *slog.Logger) *Publisher {
	if log == nil {
		log = slog.Default()
	}
	return &Publisher{pool: pool, redis: redis, dec: dec, enc: enc, log: log}
}

// Publish schedules a compile+publish, coalescing with any already pending
// within coalesceWindow. It never blocks on the compile itself.
func (p *Publisher) Publish(ctx context.Context) {
	p.mu.Lock()
	if p.pending {
		p.mu.Unlock()
		return
	}
	p.pending = true
	p.timer = time.AfterFunc(coalesceWindow, func() {
		p.mu.Lock()
		p.pending = false
		p.mu.Unlock()
		if err := p.PublishNow(context.Background()); err != nil {
			p.log.Error("catalog: publish failed", "err", err)
		}
	})
	p.mu.Unlock()
	_ = ctx
}

// publishLockKey is the pg_advisory_lock key that serializes publishers
// across processes (arbitrary constant, unique within v3).
const publishLockKey int64 = 0x7633_6361_7461 // "v3cata"

// PublishNow compiles and publishes immediately, bypassing coalescing.
//
// Compile, version bump and Redis writes run under one advisory lock held on
// a dedicated connection. Without it, two publishers could compile in one
// order and bump in the other, giving the stale snapshot the higher version.
func (p *Publisher) PublishNow(ctx context.Context) error {
	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("catalog: acquire connection: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", publishLockKey); err != nil {
		return fmt.Errorf("catalog: publish lock: %w", err)
	}
	defer func() {
		// Unlock on a fresh context so a canceled ctx cannot leak the lock.
		if _, err := conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", publishLockKey); err != nil {
			p.log.Error("catalog: publish unlock failed; closing connection", "err", err)
			_ = conn.Conn().Close(context.Background())
		}
	}()
	return p.publishLocked(ctx, conn)
}

func (p *Publisher) publishLocked(ctx context.Context, conn *pgxpool.Conn) error {
	snap, err := Compile(ctx, conn, p.dec)
	if err != nil {
		return fmt.Errorf("catalog: compile: %w", err)
	}
	sealed, err := sealSnapshot(snap, p.enc)
	if err != nil {
		return fmt.Errorf("catalog: seal snapshot: %w", err)
	}
	version, err := bumpVersion(ctx, conn)
	if err != nil {
		return fmt.Errorf("catalog: bump version: %w", err)
	}
	snap.Version = version
	sealed.Version = version

	blob, err := json.Marshal(sealed)
	if err != nil {
		return fmt.Errorf("catalog: marshal snapshot: %w", err)
	}
	// The current version never expires, so a gateway starting after a long
	// quiet period can still load it. Older versions age out.
	if err := p.redis.Set(ctx, snapshotKey(version), blob, 0).Err(); err != nil {
		return fmt.Errorf("catalog: write snapshot to redis: %w", err)
	}
	if version > 1 {
		if err := p.redis.Expire(ctx, snapshotKey(version-1), snapshotTTL).Err(); err != nil {
			p.log.Error("catalog: expire previous snapshot failed", "version", version-1, "err", err)
		}
	}
	if err := p.redis.Publish(ctx, redisx.ChannelSnapshot, fmt.Sprint(version)).Err(); err != nil {
		return fmt.Errorf("catalog: publish version: %w", err)
	}
	p.log.Info("catalog: published snapshot", "version", version, "channels", len(snap.Channels))
	return nil
}

func snapshotKey(version int64) string {
	return redisx.KeySnapshotPrefix + fmt.Sprint(version)
}

// bumpVersion atomically increments v3_platform.snapshot_versions for the
// 'catalog' kind and returns the new value.
func bumpVersion(ctx context.Context, conn *pgxpool.Conn) (int64, error) {
	var version int64
	err := conn.QueryRow(ctx, `
		INSERT INTO v3_platform.snapshot_versions (kind, version)
		VALUES ('catalog', 1)
		ON CONFLICT (kind) DO UPDATE SET version = v3_platform.snapshot_versions.version + 1,
		                                 published_at = now()
		RETURNING version`).Scan(&version)
	return version, err
}
