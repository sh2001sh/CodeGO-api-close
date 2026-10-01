package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Cipher interface {
	Encrypt([]byte) ([]byte, error)
	Decrypt([]byte) ([]byte, error)
}

type PGStore struct {
	pool   *pgxpool.Pool
	cipher Cipher
}

func NewPGStore(pool *pgxpool.Pool, cipher Cipher) *PGStore {
	return &PGStore{pool: pool, cipher: cipher}
}

// List includes only expiring, enabled OAuth credentials from enabled channels.
func (s *PGStore) List(ctx context.Context) ([]Credential, error) {
	rows, err := s.pool.Query(ctx, `SELECT cr.id, ch.provider, cr.secret, cr.expires_at,
		cr.updated_at, cr.fingerprint, ch.proxy_url FROM v3_catalog.channel_credentials cr
		JOIN v3_catalog.channels ch ON ch.id=cr.channel_id
		WHERE cr.kind='oauth' AND cr.status='enabled' AND ch.status='enabled'
		AND cr.expires_at IS NOT NULL ORDER BY cr.expires_at,cr.id`)
	if err != nil {
		return nil, fmt.Errorf("credentials: list: %w", err)
	}
	defer rows.Close()
	var result []Credential
	for rows.Next() {
		c, err := s.scan(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

func (s *PGStore) scan(row pgx.Row) (Credential, error) {
	var c Credential
	var sealed, fp []byte
	if err := row.Scan(&c.ID, &c.Provider, &sealed, &c.ExpiresAt, &c.UpdatedAt, &fp, &c.ProxyURL); err != nil {
		return c, fmt.Errorf("credentials: scan: %w", err)
	}
	var err error
	if c.Secret, err = s.cipher.Decrypt(sealed); err != nil {
		return Credential{}, fmt.Errorf("credentials: decrypt credential %d: %w", c.ID, err)
	}
	if err = json.Unmarshal(fp, &c.Fingerprint); err != nil {
		return Credential{}, fmt.Errorf("credentials: invalid fingerprint for %d", c.ID)
	}
	return c, nil
}

// Refresh locks the row through token rotation and commit. An admin replacing or
// disabling a credential wins before the lock, and stale workers never overwrite
// its replacement. The existing trigger creates a catalog invalidation outbox.
func (s *PGStore) Refresh(ctx context.Context, observed Credential, refresher Refresher) (Credential, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Credential{}, fmt.Errorf("credentials: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	c, err := s.scan(tx.QueryRow(ctx, `SELECT cr.id,ch.provider,cr.secret,cr.expires_at,
		cr.updated_at,cr.fingerprint,ch.proxy_url FROM v3_catalog.channel_credentials cr
		JOIN v3_catalog.channels ch ON ch.id=cr.channel_id
		WHERE cr.id=$1 AND cr.kind='oauth' AND cr.status='enabled' AND ch.status='enabled'
		AND cr.expires_at IS NOT NULL FOR UPDATE OF cr,ch`, observed.ID))
	if err != nil {
		if ctx.Err() != nil {
			return Credential{}, ctx.Err()
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return Credential{}, ErrChanged
		}
		return Credential{}, err
	}
	if !c.UpdatedAt.Equal(observed.UpdatedAt) {
		return Credential{}, ErrChanged
	}
	fresh, err := refresher.Refresh(ctx, c)
	if err != nil {
		return Credential{}, err
	}
	if len(fresh.Secret) == 0 || !fresh.ExpiresAt.After(time.Now()) {
		return Credential{}, fmt.Errorf("credentials: invalid refresh result for %d", c.ID)
	}
	sealed, err := s.cipher.Encrypt(fresh.Secret)
	if err != nil {
		return Credential{}, fmt.Errorf("credentials: seal: %w", err)
	}
	fp, err := json.Marshal(fresh.Fingerprint)
	if err != nil {
		return Credential{}, fmt.Errorf("credentials: fingerprint: %w", err)
	}
	// The provider already invalidated the previous refresh token. Complete this
	// bounded durable write even if shutdown or the exchange timeout just fired.
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err = tx.QueryRow(persistCtx, `UPDATE v3_catalog.channel_credentials SET secret=$2,
		expires_at=$3,fingerprint=$4 WHERE id=$1 RETURNING updated_at`, c.ID, sealed, fresh.ExpiresAt, fp).Scan(&fresh.UpdatedAt); err != nil {
		return Credential{}, fmt.Errorf("credentials: update: %w", err)
	}
	if err = tx.Commit(persistCtx); err != nil {
		return Credential{}, fmt.Errorf("credentials: commit: %w", err)
	}
	fresh.ID = c.ID
	fresh.Provider = c.Provider
	fresh.ProxyURL = c.ProxyURL
	return fresh, nil
}

// RunExclusive waits for a single process-wide PostgreSQL session lock. This
// keeps provider QPS limits global even when several background workers run.
func (s *PGStore) RunExclusive(ctx context.Context, run func(context.Context) error) error {
	const lockID int64 = 0x763363726564 // "v3cred"
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("credentials: acquire leader connection: %w", err)
	}
	defer conn.Release()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		var locked bool
		if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, lockID).Scan(&locked); err != nil {
			return err
		}
		if locked {
			break
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, unlockErr := conn.Exec(cleanup, `SELECT pg_advisory_unlock($1)`, lockID); unlockErr != nil {
			_ = conn.Conn().Close(cleanup)
		}
	}()
	leaderCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	watchDone := make(chan error, 1)
	go func() {
		for {
			select {
			case <-leaderCtx.Done():
				watchDone <- nil
				return
			case <-ticker.C:
				check, done := context.WithTimeout(leaderCtx, 5*time.Second)
				pingErr := conn.Ping(check)
				done()
				if pingErr != nil {
					cancel()
					watchDone <- pingErr
					return
				}
			}
		}
	}()
	runErr := run(leaderCtx)
	cancel()
	if watchErr := <-watchDone; watchErr != nil && ctx.Err() == nil {
		return fmt.Errorf("credentials: lost refresh leadership: %w", watchErr)
	}
	return runErr
}
