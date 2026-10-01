// Package pg provides the PostgreSQL connection pool used by v3.
package pg

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool wraps pgxpool.Pool.
type Pool struct {
	*pgxpool.Pool
}

// Config holds connection parameters. DSN is a postgres:// URL or key/value
// string; pool limits in Config override any pool_* values in the DSN.
type Config struct {
	DSN      string
	MaxConns int32
	MinConns int32
}

// Connect opens a pool and checks the minimum supported PostgreSQL version.
func Connect(ctx context.Context, cfg Config) (*Pool, error) {
	if cfg.DSN == "" {
		return nil, errors.New("pg: empty DSN")
	}
	poolConfig, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("pg: parse dsn: %w", err)
	}
	if cfg.MaxConns > 0 {
		poolConfig.MaxConns = cfg.MaxConns
	}
	if cfg.MinConns > 0 {
		poolConfig.MinConns = cfg.MinConns
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("pg: create pool: %w", err)
	}
	var version int
	if err := pool.QueryRow(ctx, "SELECT current_setting('server_version_num')::int").Scan(&version); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pg: server version check: %w", err)
	}
	if version < 150000 {
		pool.Close()
		return nil, fmt.Errorf("pg: PostgreSQL 15 or newer is required (server_version_num=%d)", version)
	}
	return &Pool{Pool: pool}, nil
}
