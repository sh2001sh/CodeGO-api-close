//go:build pgintegration

package billing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

type postingStatusFunc func(string, string) (PostingState, error)

func (f postingStatusFunc) PostingState(_ context.Context, operation, transaction string) (PostingState, error) {
	return f(operation, transaction)
}

func TestPostingCleanupFencesCallbackRetryTransaction(t *testing.T) {
	_, rdb, _, _ := setup(t, 1000)
	now := time.Now()
	id, err := ReservePosting(ctx, rdb, account, "retry", "old", 100, 1000, 0, now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	k := keysFor(account, id)
	loader := postingStatusFunc(func(operation, tx string) (PostingState, error) {
		if operation != "retry" || tx != "old" {
			t.Fatalf("state lookup %q %q", operation, tx)
		}
		// Simulate another sweeper releasing the old aborted hold, then a
		// callback retry acquiring the same reservation ID in a new PG tx.
		if _, err := sweepScript.Run(ctx, rdb, []string{k.balance, k.reservation, k.done, redisx.KeyReservationOpen, redisx.StreamBillingEvents, k.holds, redisx.KeyPostingOpen}, now.UnixMilli(), k.member, id, account, "old").Int(); err != nil {
			t.Fatal(err)
		}
		if _, err := ReservePosting(ctx, rdb, account, "retry", "new", 200, 1000, 0, now.Add(-time.Minute)); err != nil {
			t.Fatal(err)
		}
		return PostingAborted, nil
	})
	if n, err := SweepPostingHolds(ctx, rdb, loader, now, 100); err != nil || n != 0 {
		t.Fatalf("stale cleanup released newer hold: %d %v", n, err)
	}
	if bal, held := balance(t, rdb); bal != 1000 || held != 200 {
		t.Fatalf("new transaction hold changed: %d %d", bal, held)
	}
	if n, _ := rdb.ZCard(ctx, redisx.KeyPostingOpen).Result(); n != 1 {
		t.Fatal("stale cleanup removed newer business index")
	}
}

func TestPostingCleanupRetainsHoldWhenPGStatusUnavailable(t *testing.T) {
	_, rdb, _, _ := setup(t, 1000)
	now := time.Now()
	if _, err := ReservePosting(ctx, rdb, account, "unknown", "old", 100, 1000, 0, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	unavailable := errors.New("PG transaction status unavailable")
	loader := postingStatusFunc(func(_, _ string) (PostingState, error) { return PostingInProgress, unavailable })
	if _, err := SweepPostingHolds(ctx, rdb, loader, now, 100); !errors.Is(err, unavailable) {
		t.Fatalf("status error swallowed: %v", err)
	}
	if bal, held := balance(t, rdb); bal != 1000 || held != 100 {
		t.Fatalf("uncertain hold released: %d %d", bal, held)
	}
}
