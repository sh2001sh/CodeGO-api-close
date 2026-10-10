// Package redisx provides Redis clients and Lua script helpers.
package redisx

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Client wraps redis.Client with app-level conventions.
type Client struct {
	*redis.Client
}

// Config holds connection parameters.
type Config struct {
	Addr     string
	Password string
	DB       int
	PoolSize int
}

// Connect establishes a Redis client.
func Connect(cfg Config) (*Client, error) {
	if cfg.PoolSize == 0 {
		// v2 defaulted to 10, which queued requests under load (plan §3).
		cfg.PoolSize = 256
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:                  cfg.Addr,
		Password:              cfg.Password,
		DB:                    cfg.DB,
		PoolSize:              cfg.PoolSize,
		DialTimeout:           5 * time.Second,
		ReadTimeout:           3 * time.Second,
		WriteTimeout:          3 * time.Second,
		ContextTimeoutEnabled: true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}

	return &Client{Client: rdb}, nil
}
