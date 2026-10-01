//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestRedemptionConcurrentReplayCreditsOnceAndRejectsOtherUser(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	expires := now.Add(time.Hour)
	code, err := s.IssueRedemption(ctx, "welcome", 3_500_000, &expires)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	fail := make(chan error, 20)
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			amount, err := s.Redeem(ctx, 1, code.Key)
			if err == nil && amount != 3_500_000 {
				err = errors.New("incorrect grant")
			}
			fail <- err
		}()
	}
	wg.Wait()
	close(fail)
	for err := range fail {
		if err != nil {
			t.Fatal(err)
		}
	}
	var balance, entries int64
	if err = pool.QueryRow(ctx, `SELECT balance,(SELECT count(*) FROM v3_billing.ledger_entries WHERE kind='redeem') FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=1 AND kind='wallet'`).Scan(&balance, &entries); err != nil {
		t.Fatal(err)
	}
	if balance != 3_500_000 || entries != 1 {
		t.Fatalf("balance=%d entries=%d", balance, entries)
	}
	if _, err = s.Redeem(ctx, 2, code.Key); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("other user redeemed used code: %v", err)
	}
	list, err := s.ListRedemptions(ctx, 0, 10)
	if err != nil || len(list) != 1 || list[0].Key != "" || list[0].State != "used" {
		t.Fatalf("list=%+v err=%v", list, err)
	}
}

func TestExpiredAndRevokedRedemptionsNeverPost(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	expires := now.Add(time.Hour)
	old, err := s.IssueRedemption(ctx, "expires", 1_000_000, &expires)
	if err != nil {
		t.Fatal(err)
	}
	blocked, err := s.IssueRedemption(ctx, "revoked", 2_000_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RevokeRedemption(ctx, blocked.ID); err != nil {
		t.Fatal(err)
	}
	*now = expires
	for _, key := range []string{old.Key, blocked.Key} {
		if _, err = s.Redeem(ctx, 1, key); !errors.Is(err, commerce.ErrStateConflict) {
			t.Fatalf("blocked redemption err=%v", err)
		}
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_billing.ledger_entries`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("blocked codes posted %d entries", count)
	}
}
