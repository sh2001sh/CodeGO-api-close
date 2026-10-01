//go:build pgintegration

package limits

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func setup(t *testing.T) (*Controller, *gateway.Request, gateway.Target, *time.Time) {
	t.Helper()
	addr := os.Getenv("V3_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("V3_TEST_REDIS_ADDR not set")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	id := now.UnixNano()
	req := &gateway.Request{ID: "first", Principal: gateway.Principal{UserID: id, MaxConcurrency: 1, RequestsPerMinute: 2}}
	target := gateway.Target{ChannelID: id, CredentialID: id, MaxConcurrency: 1, CredentialMaxConcurrency: 1, MaxUserConcurrency: 1}
	c := New(rdb, Config{LeaseTTL: time.Minute, Now: func() time.Time { return now }})
	t.Cleanup(func() { _ = rdb.Del(context.Background(), leaseKeys(req, target)...).Err(); _ = rdb.Close() })
	return c, req, target, &now
}

// Regression: a retry must not consume another RPM slot, and repeated releases
// cannot subtract another request's active concurrency lease.
func TestLeaseIdempotencyRPMAndExpiry(t *testing.T) {
	c, req, target, now := setup(t)
	ctx := context.Background()
	if err := c.Acquire(ctx, req, target); err != nil {
		t.Fatal(err)
	}
	if err := c.Acquire(ctx, req, target); err != nil {
		t.Fatal("duplicate acquire", err)
	}
	other := *req
	other.ID = "second"
	if err := c.Acquire(ctx, &other, target); !errors.Is(err, gateway.ErrRateLimited) {
		t.Fatalf("user concurrency: %v", err)
	}
	if err := c.Release(ctx, req, target); err != nil {
		t.Fatal(err)
	}
	if err := c.Release(ctx, req, target); err != nil {
		t.Fatal(err)
	}
	if err := c.Acquire(ctx, req, target); err != nil {
		t.Fatal("retry must not consume RPM", err)
	}
	if err := c.Release(ctx, req, target); err != nil {
		t.Fatal(err)
	}
	if err := c.Acquire(ctx, &other, target); err != nil {
		t.Fatal(err)
	}
	if err := c.Release(ctx, req, target); err != nil {
		t.Fatal(err)
	}
	third := *req
	third.ID = "third"
	if err := c.Acquire(ctx, &third, target); !errors.Is(err, gateway.ErrRateLimited) {
		t.Fatal("late release removed second lease", err)
	}
	if err := c.Release(ctx, &other, target); err != nil {
		t.Fatal(err)
	}
	if err := c.Acquire(ctx, &third, target); !errors.Is(err, gateway.ErrRateLimited) {
		t.Fatal("RPM limit not enforced", err)
	}
	*now = now.Add(time.Minute)
	if err := c.Acquire(ctx, &third, target); err != nil {
		t.Fatal("expired minute still limited", err)
	}
	*now = now.Add(time.Minute)
	if err := c.Acquire(ctx, &other, target); err != nil {
		t.Fatal("expired orphan lease still limited", err)
	}
}

func TestTargetLimitFailureDoesNotLeaveUserLease(t *testing.T) {
	c, req, target, _ := setup(t)
	ctx := context.Background()
	req.Principal.MaxConcurrency = 2
	if err := c.Acquire(ctx, req, target); err != nil {
		t.Fatal(err)
	}
	other := *req
	other.ID = "other"
	if err := c.Acquire(ctx, &other, target); !errors.Is(err, gateway.ErrTargetBusy) {
		t.Fatalf("channel concurrency: %v", err)
	}
	if n := c.rdb.ZCard(ctx, leaseKeys(req, target)[0]).Val(); n != 1 {
		t.Fatalf("failed target acquisition leaked user slot: %d", n)
	}
	if n := c.rdb.ZCard(ctx, leaseKeys(req, target)[4]).Val(); n != 1 {
		t.Fatalf("failed acquisition consumed RPM: %d", n)
	}
}

func TestCredentialAndChannelUserLimits(t *testing.T) {
	for _, kind := range []string{"credential", "channel_user"} {
		t.Run(kind, func(t *testing.T) {
			c, req, target, _ := setup(t)
			req.Principal.MaxConcurrency, target.MaxConcurrency = 0, 0
			if kind == "credential" {
				target.MaxUserConcurrency = 0
			} else {
				target.CredentialMaxConcurrency = 0
			}
			ctx := context.Background()
			if err := c.Acquire(ctx, req, target); err != nil {
				t.Fatal(err)
			}
			other := *req
			other.ID = "other"
			if err := c.Acquire(ctx, &other, target); !errors.Is(err, gateway.ErrTargetBusy) {
				t.Fatalf("missing %s limit: %v", kind, err)
			}
		})
	}
}

func TestConcurrentAcquisitionNeverOversubscribes(t *testing.T) {
	c, req, target, _ := setup(t)
	req.Principal.MaxConcurrency, req.Principal.RequestsPerMinute = 0, 0
	target.MaxConcurrency, target.CredentialMaxConcurrency, target.MaxUserConcurrency = 7, 0, 0
	var admitted atomic.Int32
	var wg sync.WaitGroup
	for i := range 64 {
		wg.Go(func() {
			other := *req
			other.ID = fmt.Sprintf("req-%d", i)
			if err := c.Acquire(context.Background(), &other, target); err == nil {
				admitted.Add(1)
			} else if !errors.Is(err, gateway.ErrTargetBusy) {
				t.Errorf("acquire: %v", err)
			}
		})
	}
	wg.Wait()
	if n := admitted.Load(); n != 7 {
		t.Fatalf("concurrent admission = %d, want 7", n)
	}
}

func TestScriptErrorIsNotClassifiedAsOutage(t *testing.T) {
	c, req, target, _ := setup(t)
	ctx := context.Background()
	if err := c.rdb.Set(ctx, leaseKeys(req, target)[0], "wrong type", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	err := c.Acquire(ctx, req, target)
	if err == nil || errors.Is(err, gateway.ErrLimitsUnavailable) {
		t.Fatalf("script error classified as outage: %v", err)
	}
}
