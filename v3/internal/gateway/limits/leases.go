// Package limits enforces distributed request and concurrency limits using
// expiring Redis leases. All limits for one attempt are acquired atomically.
package limits

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

//go:embed scripts/acquire.lua
var acquireLua string

//go:embed scripts/release.lua
var releaseLua string

type Config struct {
	LeaseTTL     time.Duration // must exceed the gateway relay timeout
	RedisTimeout time.Duration
	Now          func() time.Time
}

func (c Config) withDefaults() Config {
	if c.LeaseTTL <= 0 {
		c.LeaseTTL = 31 * time.Minute
	}
	if c.RedisTimeout <= 0 {
		c.RedisTimeout = 250 * time.Millisecond
	}
	return c
}

type Controller struct {
	rdb redis.UniversalClient
	cfg Config
}

func New(rdb redis.UniversalClient, cfg Config) *Controller {
	return &Controller{rdb: rdb, cfg: cfg.withDefaults()}
}

func leaseKeys(req *gateway.Request, t gateway.Target) []string {
	return []string{
		fmt.Sprintf("%suser:%d", redisx.KeyConcurrencyPrefix, req.Principal.UserID),
		fmt.Sprintf("%schannel:%d", redisx.KeyConcurrencyPrefix, t.ChannelID),
		fmt.Sprintf("%scredential:%d", redisx.KeyConcurrencyPrefix, t.CredentialID),
		fmt.Sprintf("%schannel:%d:user:%d", redisx.KeyConcurrencyPrefix, t.ChannelID, req.Principal.UserID),
		fmt.Sprintf("%s%d", redisx.KeyUserRPMPrefix, req.Principal.UserID),
		fmt.Sprintf("%s%d:%s", redisx.KeyUserRPMRequestPrefix, req.Principal.UserID, req.ID),
	}
}

func enabled(req *gateway.Request, t gateway.Target) bool {
	return req.Principal.MaxConcurrency > 0 || req.Principal.RequestsPerMinute > 0 ||
		t.MaxConcurrency > 0 || t.CredentialMaxConcurrency > 0 || t.MaxUserConcurrency > 0
}

// Acquire uses EVAL directly so a cold script cache still requires one Redis
// round trip. A failed acquisition leaves no partial concurrency reservation.
func (c *Controller) Acquire(ctx context.Context, req *gateway.Request, t gateway.Target) error {
	if !enabled(req, t) {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.RedisTimeout)
	defer cancel()
	var now int64
	if c.cfg.Now != nil {
		now = c.cfg.Now().UnixMilli()
	}
	result, err := c.rdb.Eval(ctx, acquireLua, leaseKeys(req, t), req.ID, now,
		c.cfg.LeaseTTL.Milliseconds(), req.Principal.MaxConcurrency, t.MaxConcurrency,
		t.CredentialMaxConcurrency, t.MaxUserConcurrency, req.Principal.RequestsPerMinute).Int()
	if err != nil {
		return commandError("acquire", err)
	}
	switch result {
	case 0:
		return nil
	case 1:
		return gateway.ErrRateLimited
	case 2:
		return gateway.ErrTargetBusy
	default:
		return fmt.Errorf("limits: unexpected acquisition result %d", result)
	}
}

// Release does not require the original client context; the caller must use
// a detached, bounded context after an attempt finishes or is canceled.
func (c *Controller) Release(ctx context.Context, req *gateway.Request, t gateway.Target) error {
	if !enabled(req, t) {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.RedisTimeout)
	defer cancel()
	if err := c.rdb.Eval(ctx, releaseLua, leaseKeys(req, t)[:4], req.ID).Err(); err != nil {
		return commandError("release", err)
	}
	return nil
}

var _ gateway.LeaseController = (*Controller)(nil)

func commandError(operation string, err error) error {
	var protocolError redis.Error
	if errors.As(err, &protocolError) {
		// A script/WRONGTYPE error is an implementation or data problem, not a
		// transient connection outage. Preserve it for operator-visible logging.
		return fmt.Errorf("limits: %s: %w", operation, err)
	}
	return fmt.Errorf("%w: %s: %v", gateway.ErrLimitsUnavailable, operation, err)
}
