// Package boot opens the shared infrastructure every v3 binary needs, from
// environment variables:
//
//	V3_PG_DSN       postgres:// URL (required)
//	V3_REDIS_ADDR   host:port (required)
//	V3_REDIS_PASSWORD
//	V3_REDIS_POOL_SIZE  connections per process (default 256)
//	V3_SECRET_KEY   base64 32-byte AES-256 key for credential secrets (required)
package boot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/pkg/pg"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

// Deps holds opened connections. Close releases them.
type Deps struct {
	PG     *pg.Pool
	Redis  *redisx.Client
	Crypto *catalog.AESGCM
}

// Open connects to PostgreSQL and Redis and loads the secret key.
func Open(ctx context.Context, maxPGConns int32) (*Deps, error) {
	dsn, addr, secret := os.Getenv("V3_PG_DSN"), os.Getenv("V3_REDIS_ADDR"), os.Getenv("V3_SECRET_KEY")
	if dsn == "" || addr == "" || secret == "" {
		return nil, errors.New("V3_PG_DSN, V3_REDIS_ADDR and V3_SECRET_KEY must be set")
	}
	crypto, err := catalog.NewAESGCMFromBase64(secret)
	if err != nil {
		return nil, err
	}
	pool, err := pg.Connect(ctx, pg.Config{DSN: dsn, MaxConns: maxPGConns})
	if err != nil {
		return nil, fmt.Errorf("postgres: %w", err)
	}
	poolSize := 0 // redisx default
	if v := os.Getenv("V3_REDIS_POOL_SIZE"); v != "" {
		if poolSize, err = strconv.Atoi(v); err != nil || poolSize <= 0 {
			pool.Close()
			return nil, fmt.Errorf("V3_REDIS_POOL_SIZE must be a positive integer, got %q", v)
		}
	}
	rdb, err := redisx.Connect(redisx.Config{Addr: addr, Password: os.Getenv("V3_REDIS_PASSWORD"), PoolSize: poolSize})
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("redis: %w", err)
	}
	return &Deps{PG: pool, Redis: rdb, Crypto: crypto}, nil
}

// Close releases the connections.
func (d *Deps) Close() {
	_ = d.Redis.Close()
	d.PG.Close()
}
