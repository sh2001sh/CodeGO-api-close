//go:build pgintegration

package billing

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

type abortRequestKey struct{}

// lostReserveReply models a command that commits but loses its reply, or is
// delayed until after the caller has been told that admission failed.
type lostReserveReply struct {
	execute bool
	cancel  context.CancelFunc
	calls   atomic.Int64
}

func (*lostReserveReply) DialHook(next redis.DialHook) redis.DialHook { return next }
func (*lostReserveReply) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h *lostReserveReply) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(callCtx context.Context, cmd redis.Cmder) error {
		args := cmd.Args()
		if callCtx.Value(abortRequestKey{}) == nil || cmd.Name() != "evalsha" || len(args) < 2 ||
			(args[1] != reserveScript.Hash() && args[1] != fundingReserveScript.Hash()) {
			return next(callCtx, cmd)
		}
		h.calls.Add(1)
		if h.execute {
			if err := next(callCtx, cmd); err != nil {
				return err
			}
		}
		if h.cancel != nil {
			h.cancel()
			return context.Canceled
		}
		return context.DeadlineExceeded
	}
}

func abortFixture(t *testing.T, mode string) (*Settler, *redisx.Client, *gateway.Request, map[int64]int64) {
	t.Helper()
	s, rdb, _, clock := setup(t, 1000)
	req := newReq("lost-reserve-reply")
	want := map[int64]int64{account: 1000}
	if mode == "source" {
		var snapshot *catalog.Snapshot
		req, snapshot = sourceFixture(clock.now())
		profile := snapshot.AccountProfiles[7]
		profile.Subscriptions[0].SubscriptionID = 90
		profile.Subscriptions[0].ModelLimits = map[string]int64{"model": 700}
		snapshot.AccountProfiles[7] = profile
		s.snapshot = func() *catalog.Snapshot { return snapshot }
	}
	if mode != "wallet" {
		s.accounts = fundedResolver{sources: []int64{43}}
		req.Principal.BudgetLimited, req.Principal.BudgetAccountID = true, 44
		want[43], want[44] = 450, 1000
	}
	for id, amount := range want {
		if err := rdb.HSet(ctx, BalanceKey(id), "balance", amount, "reserved", 0, "ver", 1, "base", 1).Err(); err != nil {
			t.Fatal(err)
		}
	}
	for _, script := range []*redis.Script{reserveScript, fundingReserveScript} {
		if err := script.Load(ctx, rdb).Err(); err != nil {
			t.Fatal(err)
		}
	}
	w, err := openWAL(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.wal, s.cfg.DisableOutageAdmission = w, true
	t.Cleanup(func() {
		select {
		case <-w.stopped:
		default:
			s.Close()
		}
	})
	return s, rdb, req, want
}
