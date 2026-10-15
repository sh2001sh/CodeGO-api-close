package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

// coalesceWindow batches invalidations that arrive close together into one
// Compile+publish, so a burst of catalog writes produces one new version.
const coalesceWindow = 200 * time.Millisecond

const publishRetryDelay = time.Second

// snapshotTTL is how long a superseded snapshot blob stays in Redis, so a
// gateway that saw the old announcement late can still load it.
const snapshotTTL = time.Hour

// Keep the current snapshot and two predecessors. The TTL bounds idle
// retention, while this count bounds memory during frequent publications.
const retainedSnapshots int64 = 3

// Track successful publications rather than consecutive version numbers:
// PostgreSQL may already have bumped the version when a Redis write fails.
// The registry and blobs require the same single Redis primary as billing.
const snapshotVersionsKey = "v3:snapshot:retained"

var publishSnapshot = redis.NewScript(`
local previous = redis.call('ZREVRANGE', KEYS[2], 0, 0)[1]
redis.call('SET', KEYS[1], ARGV[1])
if previous and previous ~= KEYS[1] then
    redis.call('EXPIRE', previous, ARGV[3])
elseif not previous then
    redis.call('EXPIRE', KEYS[3], ARGV[3])
end
redis.call('ZADD', KEYS[2], ARGV[2], KEYS[1])
local excess = redis.call('ZCARD', KEYS[2]) - tonumber(ARGV[4])
if excess > 0 then
    local retired = redis.call('ZRANGE', KEYS[2], 0, excess - 1)
    for _, key in ipairs(retired) do
        redis.call('UNLINK', key)
        redis.call('ZREM', KEYS[2], key)
    end
end
redis.call('PUBLISH', ARGV[5], ARGV[2])
return 1
`)

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

	mu          sync.Mutex
	pending     bool
	dirty       bool
	timer       *time.Timer
	publishGate chan struct{}
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
	return &Publisher{pool: pool, redis: redis, dec: dec, enc: enc, log: log, publishGate: make(chan struct{}, 1)}
}

// Publish schedules a compile+publish, coalescing with any already pending
// within coalesceWindow. It never blocks on the compile itself.
func (p *Publisher) Publish(ctx context.Context) {
	p.mu.Lock()
	p.dirty = true
	if p.pending {
		p.mu.Unlock()
		return
	}
	p.pending = true
	p.timer = time.AfterFunc(coalesceWindow, func() {
		p.publishPending(func() error { return p.PublishNow(context.Background()) })
	})
	p.mu.Unlock()
	_ = ctx
}

// Keep one runner alive through the entire publication. Changes received
// while it compiles require one more pass, since its read transaction may
// predate them. A failed pass must also retry: the outbox already committed.
func (p *Publisher) publishPending(publish func() error) {
	for {
		p.mu.Lock()
		p.dirty = false
		p.mu.Unlock()
		err := publish()
		if err != nil {
			p.log.Error("catalog: publish failed; retrying", "err", err)
		}
		p.mu.Lock()
		if err == nil && !p.dirty {
			p.pending = false
			p.mu.Unlock()
			return
		}
		p.mu.Unlock()
		delay := coalesceWindow
		if err != nil {
			delay = publishRetryDelay
		}
		time.Sleep(delay)
	}
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
	// Wait before acquiring a pooled connection. Heavy snapshots can take
	// minutes; waiting publishers must leave connections available to billing.
	select {
	case p.publishGate <- struct{}{}:
		defer func() { <-p.publishGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	conn, err := p.acquirePublishLock(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	defer func() {
		// Unlock on a fresh context so a canceled ctx cannot leak the lock.
		if _, err := conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", publishLockKey); err != nil {
			p.log.Error("catalog: publish unlock failed; closing connection", "err", err)
			_ = conn.Conn().Close(context.Background())
		}
	}()
	return p.publishLocked(ctx, conn)
}

// Another process may be publishing. Never hold a pool connection while
// waiting for it: control and worker share their pool with ordinary work.
func (p *Publisher) acquirePublishLock(ctx context.Context) (*pgxpool.Conn, error) {
	for {
		conn, err := p.pool.Acquire(ctx)
		if err != nil {
			return nil, fmt.Errorf("catalog: acquire connection: %w", err)
		}
		var locked bool
		err = conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", publishLockKey).Scan(&locked)
		if err == nil && locked {
			return conn, nil
		}
		if err != nil {
			// A canceled/lost reply may hide a successfully acquired session
			// lock. Discard the connection instead of returning that lock to
			// the pool, where a later publisher could acquire it recursively.
			_ = conn.Conn().Close(context.Background())
			conn.Release()
			return nil, fmt.Errorf("catalog: publish lock: %w", err)
		}
		conn.Release()
		timer := time.NewTimer(coalesceWindow)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
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
	// Publication and retention are atomic. UNLINK releases retired blobs
	// off the Redis command thread; pre-upgrade snapshots still age out by TTL.
	// Catalog blobs are much larger than billing commands, especially at the
	// first publication after import. Keep the billing connection's short
	// timeouts and give this transfer its own bounded connection.
	options := *p.redis.Options()
	options.ReadTimeout = 90 * time.Second
	options.WriteTimeout = 90 * time.Second
	options.PoolSize = 1
	transfer := redis.NewClient(&options)
	defer transfer.Close()
	p.log.Info("catalog: snapshot encoded", "version", version, "bytes", len(blob))
	if err := publishSnapshot.Run(ctx, transfer,
		[]string{snapshotKey(version), snapshotVersionsKey, snapshotKey(version - 1)},
		blob, version, int64(snapshotTTL/time.Second), retainedSnapshots, redisx.ChannelSnapshot).Err(); err != nil {
		return fmt.Errorf("catalog: publish snapshot to redis: %w", err)
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
