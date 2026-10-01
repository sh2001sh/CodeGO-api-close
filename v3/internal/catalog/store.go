package catalog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

// pollInterval is the safety-net cadence for polling snapshot_versions when
// pub/sub delivery is missed (network blip, restart between publish and
// subscribe, ...).
const pollInterval = 30 * time.Second

// Store holds the gateway's in-memory view of the catalog. Readers call
// Current; Run keeps it fresh in the background. The zero value is not
// usable; build one with NewStore.
type Store struct {
	pool  *pgxpool.Pool
	redis *redisx.Client
	dec   Decrypter
	log   *slog.Logger

	current atomic.Pointer[Snapshot]
}

// NewStore builds a Store. Call Load once before serving traffic, then Run
// in the background to keep it current.
func NewStore(pool *pgxpool.Pool, redis *redisx.Client, dec Decrypter, log *slog.Logger) *Store {
	if log == nil {
		log = slog.Default()
	}
	return &Store{pool: pool, redis: redis, dec: dec, log: log}
}

// Current returns the latest snapshot, or nil before the first successful
// Load.
func (s *Store) Current() *Snapshot {
	return s.current.Load()
}

// Version returns the current snapshot's version, or 0 before the first
// successful Load.
func (s *Store) Version() int64 {
	if snap := s.current.Load(); snap != nil {
		return snap.Version
	}
	return 0
}

// Load fetches the latest published version and swaps it in if it is newer
// than what Store already holds. It never regresses to an older version.
func (s *Store) Load(ctx context.Context) error {
	version, err := latestVersion(ctx, s.pool)
	if err != nil {
		return fmt.Errorf("catalog: read latest version: %w", err)
	}
	return s.loadVersion(ctx, version)
}

// loadVersion loads one specific version from Redis and swaps it in if it is
// newer than the current snapshot. Versions at or below the current one are
// ignored, including the racy case where a fresher one already won.
func (s *Store) loadVersion(ctx context.Context, version int64) error {
	if version <= s.Version() {
		return nil
	}
	snap, err := s.fetch(ctx, version)
	if err != nil {
		return err
	}
	s.swap(snap)
	return nil
}

// fetch reads a published snapshot from Redis. If the blob is gone (Redis
// flushed or restarted), it compiles from PostgreSQL instead so a gateway can
// always start; the result is labeled with the version it was asked for.
func (s *Store) fetch(ctx context.Context, version int64) (*Snapshot, error) {
	blob, err := s.redis.Get(ctx, snapshotKey(version)).Bytes()
	if errors.Is(err, redis.Nil) {
		s.log.Warn("catalog: snapshot blob missing in redis; compiling from postgres", "version", version)
		snap, err := Compile(ctx, s.pool, s.dec)
		if err != nil {
			return nil, fmt.Errorf("catalog: fallback compile for %d: %w", version, err)
		}
		snap.Version = version
		return snap, nil
	}
	if err != nil {
		return nil, fmt.Errorf("catalog: read snapshot %d: %w", version, err)
	}
	w, err := decodeWireSnapshot(blob)
	if err != nil {
		return nil, fmt.Errorf("catalog: unmarshal snapshot %d: %w", version, err)
	}
	snap, err := openSnapshot(w, s.dec)
	if err != nil {
		return nil, fmt.Errorf("catalog: open snapshot %d: %w", version, err)
	}
	return snap, nil
}

// swap installs snap unless a newer version is already in place.
func (s *Store) swap(snap *Snapshot) {
	for {
		old := s.current.Load()
		if old != nil && old.Version >= snap.Version {
			return // a fresher swap already happened
		}
		if s.current.CompareAndSwap(old, snap) {
			return
		}
	}
}

// Run subscribes to ChannelSnapshot and polls snapshot_versions as a safety
// net, until ctx is canceled. A failed load logs and keeps the previous
// snapshot in place; it never blocks the caller.
func (s *Store) Run(ctx context.Context) error {
	sub := s.redis.Subscribe(ctx, redisx.ChannelSnapshot)
	defer func() { _ = sub.Close() }()
	msgs := sub.Channel()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-msgs:
			if !ok {
				return nil
			}
			s.handleAnnounce(ctx, msg.Payload)
		case <-ticker.C:
			if err := s.Load(ctx); err != nil {
				s.log.Error("catalog: safety-net poll failed", "err", err)
			}
		}
	}
}

func (s *Store) handleAnnounce(ctx context.Context, payload string) {
	version, err := parseVersion(payload)
	if err != nil {
		s.log.Error("catalog: bad snapshot announcement", "payload", payload, "err", err)
		return
	}
	if err := s.loadVersion(ctx, version); err != nil {
		s.log.Error("catalog: load announced snapshot failed", "version", version, "err", err)
	}
}

func parseVersion(payload string) (int64, error) {
	var v int64
	_, err := fmt.Sscanf(payload, "%d", &v)
	if err != nil {
		return 0, err
	}
	if v <= 0 {
		return 0, errors.New("catalog: non-positive version")
	}
	return v, nil
}

func latestVersion(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var version int64
	err := pool.QueryRow(ctx, `SELECT version FROM v3_platform.snapshot_versions WHERE kind = 'catalog'`).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil // no catalog has ever been published
	}
	return version, err
}
