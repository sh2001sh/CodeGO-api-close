//go:build pgintegration

package billing

import (
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
