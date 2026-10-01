//go:build pgintegration

package main

import (
	"context"
	"errors"
	"testing"

	"github.com/redis/go-redis/v9"
)

// Only the request's script round trip fails; Redis/pubsub and later balance
// reads remain live. A warmed local allowance would otherwise admit this call.
type outageReserveHook struct{}

func (outageReserveHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (outageReserveHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (outageReserveHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if ctx.Value(contractContextKey{}) == "request" && (cmd.Name() == "evalsha" || cmd.Name() == "eval") {
			return errors.New("simulated isolated request Redis outage")
		}
		return next(ctx, cmd)
	}
}

func TestProductionRedisOutageRefusesWarmedLocalAllowance(t *testing.T) {
	t.Setenv("V3_FILES_DIR", t.TempDir())
	f := newContractFixture(t)
	warmNativeFixture(t, f)
	before, calls := f.balances(t), f.upstreamCalls.Load()
	f.deps.Redis.AddHook(outageReserveHook{})
	w, _ := f.request(contractBody(false, "outage"), "127.0.0.1:3456")
	if w.Code != 503 {
		t.Fatalf("production Redis outage admission status=%d body=%s", w.Code, w.Body)
	}
	if f.upstreamCalls.Load() != calls {
		t.Fatal("Redis outage bypassed shared budgets and reached upstream")
	}
	assertContractDebit(t, before, f.balances(t), 0)
	f.assertNoHolds(t)
}
