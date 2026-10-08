//go:build pgintegration

package incentives

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
)

func TestCompetingLuckyWorkersPostExactlyOnce(t *testing.T) {
	s, pool, _ := fixture(t)
	seedDraw(t, s, 3000000)
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- s.RetryDraw(ctx, 100) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var balance, entries, notices int64
	var number, status string
	err := pool.QueryRow(ctx, `SELECT (SELECT balance FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=1 AND kind='wallet'),(SELECT count(*) FROM v3_billing.ledger_entries),(SELECT count(*) FROM v3_commerce.subscription_lucky_reward_notifications),winning_number,status FROM v3_commerce.subscription_lucky_draws WHERE id=100`).Scan(&balance, &entries, &notices, &number, &status)
	if err != nil || balance != 3000000 || entries != 1 || notices != 1 || number != "1234" || status != "completed" {
		t.Fatalf("balance=%d entries=%d notices=%d winning=%s status=%s err=%v", balance, entries, notices, number, status, err)
	}
	if err = s.MarkRead(ctx, 2, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user read allowed: %v", err)
	}
	if _, err = s.History(ctx, 1, 1, 20); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Notifications(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PublicWins(ctx, "", 1, 20); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AdminDraws(ctx, 1, 20); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Self(ctx, 1); !errors.Is(err, ErrRetired) {
		t.Fatalf("retired participation remains available: %v", err)
	}
}

type failedPoster struct{}

func (failedPoster) PostTx(context.Context, pgx.Tx, billing.Entry) (billing.PostResult, error) {
	return billing.PostResult{}, errors.New("posting intentionally unavailable")
}
func TestFailedLuckyPostRollsBackAndRetryPreservesNumber(t *testing.T) {
	s, pool, _ := fixture(t)
	seedDraw(t, s, 3000000)
	s.poster = failedPoster{}
	ctx := context.Background()
	if err := s.RetryDraw(ctx, 100); err == nil {
		t.Fatal("failed post reported success")
	}
	var entries, notices int
	var status, winning string
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM v3_billing.ledger_entries),(SELECT count(*) FROM v3_commerce.subscription_lucky_reward_notifications),credit_status FROM v3_commerce.subscription_lucky_rewards WHERE id=100`).Scan(&entries, &notices, &status); err != nil || entries != 0 || notices != 0 || status != "pending" {
		t.Fatalf("failed transaction leaked entries=%d notices=%d status=%s err=%v", entries, notices, status, err)
	}
	s.poster = ledger.NewPoster(pool)
	if _, err := s.UpdateSettings(ctx, map[string]json.RawMessage{"base_reward_2_usd": json.RawMessage("999")}); err != nil {
		t.Fatal(err)
	}
	if err := s.RetryDraw(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT winning_number FROM v3_commerce.subscription_lucky_draws WHERE id=100`).Scan(&winning); err != nil || winning != "1234" {
		t.Fatalf("winning=%s: %v", winning, err)
	}
	var balance int64
	if err := pool.QueryRow(ctx, `SELECT balance FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=1 AND kind='wallet'`).Scan(&balance); err != nil || balance != 3000000 {
		t.Fatalf("changed config altered frozen reward: %d %v", balance, err)
	}
}
func TestRetiredLuckyRecoveryConcurrentNeverCreatesDrawsOrNumbers(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	seedDraw(t, s, 3000000)
	if _, err := s.Backfill(ctx); !errors.Is(err, ErrRetired) {
		t.Fatalf("retired backfill: %v", err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- s.RecoverLuckyRewards(ctx) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.subscription_lucky_draws `).Scan(&count); err != nil || count != 1 {
		t.Fatalf("retirement created a draw: count=%d err=%v", count, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.subscription_lucky_numbers`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("retirement created numbers: count=%d err=%v", count, err)
	}
}
