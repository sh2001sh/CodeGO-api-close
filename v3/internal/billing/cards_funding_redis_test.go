//go:build pgintegration

package billing

import (
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

func TestConsumptionFundingAndKeyBudgetShareOnePrimaryAudit(t *testing.T) {
	s, rdb, _, clock := setup(t, 2000)
	snapshot := cardSnapshot(clock.now(), 1000, consumptionCard())
	profile := snapshot.AccountProfiles[7]
	profile.Subscriptions = []catalog.SubscriptionBucket{{AccountID: 43, StartsAt: clock.now().Add(-time.Hour), ExpiresAt: clock.now().Add(time.Hour)}}
	snapshot.AccountProfiles[7] = profile
	s.snapshot = func() *catalog.Snapshot { return snapshot }
	for id, amount := range map[int64]int64{43: 600, 44: 2000} {
		if err := rdb.HSet(ctx, BalanceKey(id), "balance", amount, "reserved", 0, "ver", 0, "base", 0).Err(); err != nil {
			t.Fatal(err)
		}
	}
	req := cardRequest("consume-funded")
	req.Principal.BudgetLimited, req.Principal.BudgetAccountID = true, 44
	if err := s.Reserve(ctx, req); err != nil {
		t.Fatal(err)
	}
	if _, held := balance(t, rdb); held != 300 {
		t.Fatalf("discounted wallet hold=%d", held)
	}
	if err := s.Finalize(ctx, req, completed(0, 0)); err != nil {
		t.Fatal(err)
	}
	if bal, held := balance(t, rdb); bal != 1700 || held != 0 {
		t.Fatalf("wallet=%d reserved=%d", bal, held)
	}
	for id, want := range map[int64]int64{43: 0, 44: 1100} {
		if bal, _ := rdb.HGet(ctx, BalanceKey(id), "balance").Int64(); bal != want {
			t.Fatalf("account %d balance=%d want=%d", id, bal, want)
		}
		if held, _ := rdb.HGet(ctx, BalanceKey(id), "reserved").Int64(); held != 0 {
			t.Fatalf("account %d held=%d", id, held)
		}
	}
	cardRecords := 0
	for _, event := range events(t, rdb) {
		if event["usage_total_amount"] != "900" {
			t.Fatalf("funded usage=%v", event)
		}
		if event[FieldCardID] != nil {
			cardRecords++
			if event["funding_part"] != "primary" || event[FieldCardBefore] != "1000" || event[FieldCardAfter] != "900" {
				t.Fatalf("aggregate card audit appeared on wrong funding part: %v", event)
			}
		}
	}
	if cardRecords != 1 {
		t.Fatalf("card event count=%d", cardRecords)
	}
	if err := s.Finalize(ctx, req, completed(0, 0)); err != nil {
		t.Fatal(err)
	}
	if len(events(t, rdb)) != 3 {
		t.Fatal("funded redelivery duplicated usage or card audits")
	}
}

func TestConsumptionWALReplayPreservesAuditExactlyOnce(t *testing.T) {
	s, rdb, _, clock := setup(t, 5000)
	snapshot := cardSnapshot(clock.now(), 1000, consumptionCard())
	s.snapshot = func() *catalog.Snapshot { return snapshot }
	req := cardRequest("consume-wal")
	if err := s.Reserve(ctx, req); err != nil {
		t.Fatal(err)
	}
	w, err := openWAL(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.wal = w
	t.Cleanup(s.Close)
	for range 3 {
		s.br.fail()
	}
	for range 2 {
		if err := s.Finalize(ctx, req, completed(0, 0)); err != nil {
			t.Fatal(err)
		}
	}
	if len(events(t, rdb)) != 0 {
		t.Fatal("outage settlement escaped WAL")
	}
	if err := s.Replay(ctx); err != nil {
		t.Fatal(err)
	}
	if bal, held := balance(t, rdb); bal != 4100 || held != 0 {
		t.Fatalf("WAL charged=%d reserved=%d", bal, held)
	}
	all := events(t, rdb)
	if len(all) != 1 || all[0][FieldCardID] != "81" || all[0][FieldCardBefore] != "1000" || all[0][FieldCardAfter] != "900" {
		t.Fatalf("WAL audit duplicated or lost: %v", all)
	}
}
