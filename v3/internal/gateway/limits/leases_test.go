package limits

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestUnavailableFailsClosedAndUnlimitedSkipsRedis(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", MaxRetries: -1, DialTimeout: time.Millisecond})
	t.Cleanup(func() { _ = rdb.Close() })
	c := New(rdb, Config{RedisTimeout: 10 * time.Millisecond})
	req := &gateway.Request{ID: "test", Principal: gateway.Principal{UserID: 1}}
	if err := c.Acquire(context.Background(), req, gateway.Target{}); err != nil {
		t.Fatal(err)
	}
	req.Principal.MaxConcurrency = 1
	if err := c.Acquire(context.Background(), req, gateway.Target{}); !errors.Is(err, gateway.ErrLimitsUnavailable) {
		t.Fatalf("unavailable Redis allowed limited request: %v", err)
	}
}
