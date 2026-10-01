package identity

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

// Config tunes the authorizer. Zero values select defaults.
type Config struct {
	L1Entries        int           // default 200k
	L1TTL            time.Duration // default 60 s, ±20% jitter; bounds staleness if pub/sub drops a message
	NegativeTTL      time.Duration // default 30 s, L1 only
	L2TTL            time.Duration // default 10 min, ±20% jitter
	MaxDBConcurrency int           // default 32 concurrent PostgreSQL lookups
	MaxDBWaiters     int           // default 4096 queued lookups; beyond it fail fast
	DBWait           time.Duration // default 1 s queueing budget per lookup
	LoadTimeout      time.Duration // default 3 s, whole L2+PG fetch
	RedisTimeout     time.Duration // default 250 ms per L2 call, so a hung Redis leaves budget for PG
	RedisBackoff     time.Duration // default 1 s: skip L2 after a Redis failure
	TouchWindow      time.Duration // default 30 s
	Now              func() time.Time
}

func (c Config) withDefaults() Config {
	c.L1Entries = cmp(c.L1Entries, 200_000)
	c.L1TTL = cmp(c.L1TTL, time.Minute)
	c.NegativeTTL = cmp(c.NegativeTTL, 30*time.Second)
	c.L2TTL = cmp(c.L2TTL, 10*time.Minute)
	c.MaxDBConcurrency = cmp(c.MaxDBConcurrency, 32)
	c.MaxDBWaiters = cmp(c.MaxDBWaiters, 4096)
	c.DBWait = cmp(c.DBWait, time.Second)
	c.LoadTimeout = cmp(c.LoadTimeout, 3*time.Second)
	c.RedisTimeout = cmp(c.RedisTimeout, 250*time.Millisecond)
	c.RedisBackoff = cmp(c.RedisBackoff, time.Second)
	c.TouchWindow = cmp(c.TouchWindow, 30*time.Second)
	if c.Now == nil {
		c.Now = time.Now
	}
	return c
}

func cmp[T int | time.Duration](v, def T) T {
	if v <= 0 {
		return def
	}
	return v
}

// remote and loader are the L2 and PostgreSQL tiers; tests substitute fakes.
type remote interface {
	get(ctx context.Context, hash [32]byte) (*KeyProfile, bool, error)
	put(ctx context.Context, hash [32]byte, p *KeyProfile, loadStart int64) (bool, error)
	clock(ctx context.Context) (int64, error)
	invalidateKey(ctx context.Context, keyID int64) error
	invalidateUser(ctx context.Context, userID int64) error
}

type loader interface {
	load(ctx context.Context, hash [32]byte) (*KeyProfile, error)
}

// Authorizer implements gateway.Authorizer.
type Authorizer struct {
	cfg     Config
	l1      *lru
	idx     *index
	l2      remote
	db      loader
	flight  flight
	sem     chan struct{}
	waiters atomic.Int64
	epoch   atomic.Uint64 // bumped on every invalidation; fences L1 fills
	touch   *toucher
	// redisDownUntil (unix nanos): L2 is skipped until then after a failure.
	redisDownUntil atomic.Int64

	ready     chan struct{}
	readyOnce sync.Once
	rdb       *redisx.Client
	log       *slog.Logger
}

// New builds an Authorizer backed by PostgreSQL and Redis.
func New(pool *pgxpool.Pool, rdb *redisx.Client, cfg Config, log *slog.Logger) *Authorizer {
	cfg = cfg.withDefaults()
	if log == nil {
		log = slog.Default()
	}
	remote := &l2{rdb: rdb, ttl: cfg.L2TTL, tomb: 2*cfg.L2TTL + cfg.LoadTimeout}
	a := newAuthorizer(cfg, remote, pgLoader{pool: pool}, log)
	a.rdb = rdb
	a.touch = newToucher(pool, cfg.TouchWindow, cfg.Now, log)
	return a
}

func newAuthorizer(cfg Config, remote remote, db loader, log *slog.Logger) *Authorizer {
	a := &Authorizer{cfg: cfg, idx: newIndex(), l2: remote, db: db, sem: make(chan struct{}, cfg.MaxDBConcurrency),
		log: log, ready: make(chan struct{})}
	a.l1 = newLRU(cfg.L1Entries, a.idx.drop)
	return a
}

// Authorize resolves an API key. Unknown, disabled and expired keys return
// gateway.ErrInvalidKey; backend trouble returns gateway.ErrAuthUnavailable.
func (a *Authorizer) Authorize(ctx context.Context, apiKey string) (gateway.Principal, error) {
	p, err := a.Profile(ctx, apiKey)
	if err != nil {
		return gateway.Principal{}, err
	}
	return p.Principal(), nil
}

// Profile is Authorize with the full key profile, for model/CIDR checks.
func (a *Authorizer) Profile(ctx context.Context, apiKey string) (*KeyProfile, error) {
	hash := HashKey(apiKey)
	hs := string(hash[:])
	now := a.cfg.Now()
	p, hit := a.l1.get(hs, now.UnixNano())
	if !hit {
		var err error
		if p, err = a.flight.do(hs, func() (*KeyProfile, error) { return a.fetch(ctx, hash) }); err != nil {
			return nil, err
		}
	}
	if p == nil {
		return nil, gateway.ErrInvalidKey
	}
	if err := p.usable(now); err != nil {
		return nil, err
	}
	if a.touch != nil {
		a.touch.touch(p.KeyID)
	}
	return p, nil
}

// fetch fills L1 from L2 or PostgreSQL. It runs once per key among
// concurrent callers, detached from any single caller's cancellation.
func (a *Authorizer) fetch(parent context.Context, hash [32]byte) (*KeyProfile, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), a.cfg.LoadTimeout)
	defer cancel()
	epoch := a.epoch.Load()
	hs := string(hash[:])

	redisUp := a.redisUsable()
	if redisUp {
		rctx, rcancel := context.WithTimeout(ctx, a.cfg.RedisTimeout)
		p, ok, err := a.l2.get(rctx, hash)
		rcancel()
		if err != nil {
			a.redisFailed("l2 read", err)
			redisUp = false
		} else if ok {
			a.fillL1(hs, p, epoch)
			return p, nil
		}
	}

	var loadStart int64
	if redisUp {
		rctx, rcancel := context.WithTimeout(ctx, a.cfg.RedisTimeout)
		var err error
		loadStart, err = a.l2.clock(rctx)
		rcancel()
		if err != nil {
			a.redisFailed("l2 clock", err)
			redisUp = false
		}
	}
	p, err := a.loadDB(ctx, hash)
	if err != nil {
		return nil, err
	}
	if p == nil {
		a.fillL1(hs, nil, epoch) // negative entries never go to Redis
		return nil, nil
	}
	if redisUp {
		rctx, rcancel := context.WithTimeout(ctx, a.cfg.RedisTimeout)
		written, err := a.l2.put(rctx, hash, p, loadStart)
		rcancel()
		switch {
		case err != nil:
			a.redisFailed("l2 write", err)
		case !written:
			return p, nil // invalidated while loading: serve this request, cache nothing
		}
	}
	// Without Redis the L1 entry still expires within L1TTL, which bounds
	// staleness from invalidations missed during the outage.
	a.fillL1(hs, p, epoch)
	return p, nil
}

func (a *Authorizer) redisUsable() bool {
	return a.cfg.Now().UnixNano() >= a.redisDownUntil.Load()
}

// redisFailed skips L2 for RedisBackoff so a hung Redis costs at most one
// RedisTimeout per backoff window instead of one per lookup.
func (a *Authorizer) redisFailed(op string, err error) {
	until := a.cfg.Now().Add(a.cfg.RedisBackoff).UnixNano()
	if prev := a.redisDownUntil.Swap(until); prev < a.cfg.Now().UnixNano() {
		a.log.Warn("identity: redis unavailable; using postgres and l1 only", "op", op, "err", err,
			"retry_in", a.cfg.RedisBackoff)
	}
}

// loadDB bounds PostgreSQL pressure two ways: at most MaxDBConcurrency loads
// run, and at most MaxDBWaiters queue for a slot, each for up to DBWait.
// Beyond either limit it fails fast. A burst of cold keys (gateway restart,
// empty Redis) is absorbed by the short queue instead of turning into 503s;
// a sustained flood still sheds load immediately.
func (a *Authorizer) loadDB(ctx context.Context, hash [32]byte) (*KeyProfile, error) {
	select {
	case a.sem <- struct{}{}:
	default:
		if a.waiters.Add(1) > int64(a.cfg.MaxDBWaiters) {
			a.waiters.Add(-1)
			return nil, fmt.Errorf("%w: database lookup queue full", gateway.ErrAuthUnavailable)
		}
		timer := time.NewTimer(a.cfg.DBWait)
		select {
		case a.sem <- struct{}{}:
			timer.Stop()
			a.waiters.Add(-1)
		case <-timer.C:
			a.waiters.Add(-1)
			return nil, fmt.Errorf("%w: database lookups saturated", gateway.ErrAuthUnavailable)
		case <-ctx.Done():
			timer.Stop()
			a.waiters.Add(-1)
			return nil, fmt.Errorf("%w: %v", gateway.ErrAuthUnavailable, ctx.Err())
		}
	}
	defer func() { <-a.sem }()
	p, err := a.db.load(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", gateway.ErrAuthUnavailable, err)
	}
	return p, nil
}

// fillL1 caches p unless an invalidation arrived since the fetch began.
func (a *Authorizer) fillL1(hs string, p *KeyProfile, epoch uint64) {
	if a.epoch.Load() != epoch {
		return
	}
	now := a.cfg.Now().UnixNano()
	if p == nil {
		a.l1.put(hs, nil, now+int64(a.cfg.NegativeTTL))
		return
	}
	a.idx.add(hs, p)
	a.l1.put(hs, p, now+int64(jitter(a.cfg.L1TTL)))
}
