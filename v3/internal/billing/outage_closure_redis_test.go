//go:build pgintegration

package billing

import (
	"errors"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestClosedWalletCannotSpendCachedOutageAllowanceButSettlesWAL(t *testing.T) {
	s, direct, proxy := outageSetup(t, 100_000)
	s.cfg.DisableOutageAdmission = true
	request := reqFor(7, "before-close")
	if err := s.Reserve(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := direct.HSet(ctx, BalanceKey(7), "closed", "1").Err(); err != nil {
		t.Fatal(err)
	}
	proxy.cut()
	for range 3 {
		s.br.fail()
	}
	refused := reqFor(7, "closed-during-outage")
	if err := s.Reserve(ctx, refused); !errors.Is(err, gateway.ErrBillingUnavailable) {
		t.Fatalf("closed cached wallet admitted during outage: %v", err)
	}
	if refused.Reserve != nil {
		t.Fatal("refused request retained a local hold")
	}
	// Distributed closure cannot erase already accepted upstream work.
	if err := s.Finalize(ctx, request, completed(10, 20)); err != nil {
		t.Fatal(err)
	}
	if s.wal.pending.Load() != 1 {
		t.Fatal("accepted work was not durably queued")
	}
	proxy.restore()
	if err := s.Replay(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Replay(ctx); err != nil {
		t.Fatal(err)
	}
	if len(events(t, direct)) != 1 {
		t.Fatal("WAL recovery duplicated or lost accepted usage")
	}
	if closed, _ := direct.HGet(ctx, BalanceKey(7), "closed").Result(); closed != "1" {
		t.Fatal("WAL recovery reopened a closed wallet")
	}
}
