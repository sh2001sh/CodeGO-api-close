//go:build pgintegration

package billing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestReservedReconstructionPreservesLiveHoldsAndRepairsLostHashes(t *testing.T) {
	s, rdb, _, _ := setup(t, 1000)
	first, second := newReq("first"), newReq("second")
	if err := s.Reserve(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := s.Reserve(ctx, second); err != nil {
		t.Fatal(err)
	}
	if err := rdb.Del(ctx, keysFor(account, "first").reservation).Err(); err != nil {
		t.Fatal(err)
	}
	fixed, err := RecomputeReserved(ctx, rdb, account)
	if err != nil || !fixed {
		t.Fatalf("repair %t %v", fixed, err)
	}
	if bal, held := balance(t, rdb); bal != 1000 || held != 208 {
		t.Fatalf("balance=%d held=%d", bal, held)
	}
	if err := s.Finalize(ctx, second, completed(10, 20)); err != nil {
		t.Fatal(err)
	}
	if bal, held := balance(t, rdb); bal != 950 || held != 0 {
		t.Fatalf("live hold changed: %d %d", bal, held)
	}
	// A lost reservation can still settle late. Rebuilding reserved must not
	// mark the request done or forgive work already performed upstream.
	if err := s.Finalize(ctx, first, completed(10, 20)); err != nil {
		t.Fatal(err)
	}
	if bal, held := balance(t, rdb); bal != 900 || held != 0 {
		t.Fatalf("late settle=%d held=%d", bal, held)
	}
}

func TestReservedReconstructionRepairsModelCountersAndPreservesUsage(t *testing.T) {
	s, rdb, _, clock := setup(t, 1000)
	req, snapshot := sourceFixture(clock.now())
	profile := snapshot.AccountProfiles[7]
	profile.Subscriptions[0].ModelLimits = map[string]int64{"model": 900}
	profile.Subscriptions[0].ModelUsage = map[string]int64{"model": 20}
	snapshot.AccountProfiles[7] = profile
	s.snapshot = func() *catalog.Snapshot { return snapshot }
	if err := s.Reserve(ctx, req); err != nil {
		t.Fatal(err)
	}
	model := sourceLimits(profile, "model")[43].ModelKey
	const liveAmount int64 = 9_007_199_254_740_993
	liveHash := keysFor(43, "still-live").reservation
	if err := rdb.HSet(ctx, liveHash, "amount", liveAmount, "model_key", model, "model_amount", liveAmount).Err(); err != nil {
		t.Fatal(err)
	}
	if err := rdb.SAdd(ctx, ReservationIndexKey(43), liveHash).Err(); err != nil {
		t.Fatal(err)
	}
	if err := rdb.Del(ctx, keysFor(43, req.ID).reservation).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SweepExpired(ctx, 100); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		fixed, err := RecomputeReserved(ctx, rdb, 43)
		if err != nil || fixed != (i == 0) {
			t.Fatalf("recompute %d fixed=%t err=%v", i, fixed, err)
		}
	}
	for _, field := range []string{"reserved", model + ":reserved"} {
		if got, err := rdb.HGet(ctx, BalanceKey(43), field).Int64(); err != nil || got != liveAmount {
			t.Fatalf("%s=%d err=%v", field, got, err)
		}
	}
	if got, _ := rdb.HGet(ctx, BalanceKey(43), model+":used").Int64(); got != 20 {
		t.Fatalf("model usage was changed: %d", got)
	}
	if err := rdb.Del(ctx, liveHash).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := RecomputeReserved(ctx, rdb, 43); err != nil {
		t.Fatal(err)
	}
	if got, _ := rdb.HGet(ctx, BalanceKey(43), model+":reserved").Int64(); got != 0 {
		t.Fatalf("lost model hold remains: %d", got)
	}
}

func TestReservationSurvivesLongSessionDeadline(t *testing.T) {
	s, rdb, _, clock := setup(t, 1000)
	clock.ms.Store(time.Now().UnixMilli())
	long, cancel := context.WithTimeout(ctx, time.Hour)
	defer cancel()
	req := newReq("long-session")
	if err := s.Reserve(long, req); err != nil {
		t.Fatal(err)
	}
	clock.ms.Add(36 * time.Minute.Milliseconds())
	if released, err := s.SweepExpired(ctx, 100); err != nil || released != 0 {
		t.Fatalf("live hold swept: released=%d err=%v", released, err)
	}
	if err := s.Finalize(ctx, req, completed(10, 20)); err != nil {
		t.Fatal(err)
	}
	if bal, held := balance(t, rdb); bal != 950 || held != 0 {
		t.Fatalf("long session finalization=%d/%d", bal, held)
	}
}

func TestFundingReservationSurvivesLongSessionDeadline(t *testing.T) {
	for _, sourcePricing := range []bool{false, true} {
		t.Run(map[bool]string{false: "key_budget", true: "source_pricing"}[sourcePricing], func(t *testing.T) {
			s, rdb, _, clock := setup(t, 1000)
			clock.ms.Store(time.Now().UnixMilli())
			long, cancel := context.WithTimeout(ctx, time.Hour)
			defer cancel()
			req := newReq("funding-long-session")
			if sourcePricing {
				var snapshot *catalog.Snapshot
				req, snapshot = sourceFixture(clock.now())
				s.snapshot = func() *catalog.Snapshot { return snapshot }
				if err := rdb.HSet(ctx, BalanceKey(43), "balance", 450, "reserved", 0, "ver", 0).Err(); err != nil {
					t.Fatal(err)
				}
			}
			req.Principal.BudgetLimited, req.Principal.BudgetAccountID = true, 44
			if err := rdb.HSet(ctx, BalanceKey(44), "balance", 1000, "reserved", 0, "ver", 0).Err(); err != nil {
				t.Fatal(err)
			}
			if err := s.Reserve(long, req); err != nil {
				t.Fatal(err)
			}
			clock.ms.Add(36 * time.Minute.Milliseconds())
			if released, err := s.SweepExpired(ctx, 100); err != nil || released != 0 {
				t.Fatalf("live funding hold swept: released=%d err=%v", released, err)
			}
			out := completed(10, 20)
			wantWallet, wantBudget := int64(950), int64(950)
			if sourcePricing {
				out.Target = &req.Targets[0]
				wantWallet, wantBudget = 955, 505
			}
			if err := s.Finalize(ctx, req, out); err != nil {
				t.Fatal(err)
			}
			if bal, held := balance(t, rdb); bal != wantWallet || held != 0 {
				t.Fatalf("wallet finalization=%d/%d", bal, held)
			}
			if got, _ := rdb.HGet(ctx, BalanceKey(44), "balance").Int64(); got != wantBudget {
				t.Fatalf("budget balance=%d want=%d", got, wantBudget)
			}
		})
	}
}

func TestReserveRejectsMissingPriceAndFreezesAdmittedPrice(t *testing.T) {
	s, rdb, _, _ := setup(t, 1000)
	snapshot := snap
	s.snapshot = func() *catalog.Snapshot { return snapshot }
	request := newReq("invalid")
	request.Model = "unknown"
	if err := s.Reserve(ctx, request); !errors.Is(err, gateway.ErrBillingUnavailable) {
		t.Fatalf("missing price=%v", err)
	}
	request = newReq("frozen")
	if err := s.Reserve(ctx, request); err != nil {
		t.Fatal(err)
	}
	snapshot = &catalog.Snapshot{} // replacement during upstream execution
	if err := s.Finalize(ctx, request, completed(10, 20)); err != nil {
		t.Fatal(err)
	}
	if bal, _ := balance(t, rdb); bal != 950 {
		t.Fatalf("admitted price changed: %d", bal)
	}
}

// Regression: Lua tonumber rounds 2^53+1, losing one micro-credit on every
// reserve/finalize. The ledger must retain the exact bigint amount end to end.
func TestLuaMoneyPreservesBigintPrecision(t *testing.T) {
	const charge int64 = 9_007_199_254_740_993
	s, rdb, _, _ := setup(t, charge+1)
	s.snapshot = func() *catalog.Snapshot {
		return &catalog.Snapshot{Groups: snap.Groups, Prices: map[string]catalog.Price{"gpt": {Mode: "per_request", PerRequest: charge}}}
	}
	request := newReq("bigint")
	if err := s.Reserve(ctx, request); err != nil {
		t.Fatal(err)
	}
	if _, held := balance(t, rdb); held != charge {
		t.Fatalf("rounded hold: %d", held)
	}
	if err := s.Reserve(ctx, newReq("overspend")); !errors.Is(err, gateway.ErrInsufficientCredits) {
		t.Fatalf("bigint admission: %v", err)
	}
	if err := s.Finalize(ctx, request, completed(1, 1)); err != nil {
		t.Fatal(err)
	}
	if bal, held := balance(t, rdb); bal != 1 || held != 0 {
		t.Fatalf("rounded settlement balance=%d held=%d", bal, held)
	}
	if got := events(t, rdb)[0][FieldAmount]; got != "9007199254740993" {
		t.Fatalf("event amount %v", got)
	}
}

func TestMultiplierCardUsesActualChannelAndFrozenProfile(t *testing.T) {
	for _, eligible := range []bool{true, false} {
		s, rdb, _, clock := setup(t, 1000)
		snapshot := &catalog.Snapshot{Groups: snap.Groups, Prices: snap.Prices,
			Channels:        map[int64]*catalog.Channel{3: {ID: 3, MultiplierCardSupported: true, MultiplierCardUserEnabled: true}, 4: {ID: 4}},
			AccountProfiles: map[int64]catalog.AccountProfile{7: {WalletAccountID: account, Cards: []catalog.MultiplierCard{{ID: 81, PropType: "consume_discount_10", MultiplierPPM: 500000, ExpiresAt: clock.now().Add(time.Hour)}}}}}
		s.snapshot = func() *catalog.Snapshot { return snapshot }
		request := newReq("card")
		request.Targets = []gateway.Target{{ChannelID: 3}, {ChannelID: 4}}
		if err := s.Reserve(ctx, request); err != nil {
			t.Fatal(err)
		}
		if _, held := balance(t, rdb); held != 208 {
			t.Fatalf("retry to ineligible channel under-reserved: %d", held)
		}
		snapshot = &catalog.Snapshot{} // card/profile updates while upstream runs
		out := completed(10, 20)
		want := int64(975)
		if !eligible {
			out.Target.ChannelID = 4
			want = 950
		}
		if err := s.Finalize(ctx, request, out); err != nil {
			t.Fatal(err)
		}
		if bal, _ := balance(t, rdb); bal != want {
			t.Fatalf("eligible=%t balance=%d want=%d", eligible, bal, want)
		}
	}
}
