//go:build pgintegration

package billing

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

func preferenceFixture(t *testing.T, wallet, sub int64, mode string) (*Settler, *redisx.Client, *gateway.Request, *catalog.Snapshot, *clock) {
	t.Helper()
	s, rdb, _, clock := setup(t, wallet)
	req, snapshot := sourceFixture(clock.now())
	profile := snapshot.AccountProfiles[7]
	profile.BillingPreference = mode
	profile.Subscriptions[0].SubscriptionID = 9007199254740993
	snapshot.AccountProfiles[7] = profile
	s.snapshot = func() *catalog.Snapshot { return snapshot }
	if err := rdb.HSet(ctx, BalanceKey(43), "balance", sub, "reserved", 0, "ver", 0).Err(); err != nil {
		t.Fatal(err)
	}
	return s, rdb, req, snapshot, clock
}

func TestFundingPreferenceRedisSourcesAndCanonicalOrder(t *testing.T) {
	for _, tc := range []struct {
		mode                                    string
		wallet, sub, wantWallet, wantSub, total int64
	}{
		{"subscription_first", 1000, 1000, 1000, 100, 900},
		{"wallet_first", 1000, 1000, 910, 1000, 90},
		{"wallet_first", 30, 1000, 0, 400, 630},
		{"subscription_only", 1000, 1000, 1000, 100, 900},
		{"wallet_only", 1000, 1000, 910, 1000, 90},
	} {
		t.Run(fmt.Sprintf("%s-%d", tc.mode, tc.wallet), func(t *testing.T) {
			s, rdb, req, snapshot, _ := preferenceFixture(t, tc.wallet, tc.sub, tc.mode)
			if tc.mode == "wallet_only" {
				snapshot.Channels[1].Settings = nil
			} // excluded source does not require source policy
			if err := s.Reserve(ctx, req); err != nil {
				t.Fatal(err)
			}
			out := gateway.Outcome{Charge: true, Target: &req.Targets[0]}
			for range 2 {
				if err := s.Finalize(ctx, req, out); err != nil {
					t.Fatal(err)
				}
			}
			if b, h := balance(t, rdb); b != tc.wantWallet || h != 0 {
				t.Fatalf("wallet=%d/%d", b, h)
			}
			if b, _ := rdb.HGet(ctx, BalanceKey(43), "balance").Int64(); b != tc.wantSub {
				t.Fatalf("sub=%d", b)
			}
			for _, e := range events(t, rdb) {
				if e["usage_total_amount"] != fmt.Sprint(tc.total) || e[FieldFundingPreference] != tc.mode || e[FieldMarketGross] != "90" {
					t.Fatalf("event=%#v", e)
				}
			}
		})
	}
	s, rdb, req, snapshot, _ := preferenceFixture(t, 1000, 1000, "subscription_first")
	profile := snapshot.AccountProfiles[7]
	profile.FundingSourceOrder = []string{"wallet", "subscription"}
	snapshot.AccountProfiles[7] = profile
	if err := s.Reserve(ctx, req); err != nil {
		t.Fatal(err)
	}
	if _, held := balance(t, rdb); held != 90 {
		t.Fatalf("canonical wallet hold=%d", held)
	}
}

func TestFundingPreferenceRedisOnlyModeRejectsNoPartialHold(t *testing.T) {
	for _, tc := range []struct {
		name, mode       string
		wallet, sub      int64
		noSub, forbidden bool
	}{
		{"insufficient-sub", "subscription_only", 1000, 899, false, false},
		{"forbidden-sub", "subscription_only", 1000, 1000, false, true},
		{"no-sub", "subscription_only", 1000, 1000, true, false},
		{"insufficient-wallet", "wallet_only", 89, 1000, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, rdb, req, snapshot, _ := preferenceFixture(t, tc.wallet, tc.sub, tc.mode)
			if tc.noSub {
				p := snapshot.AccountProfiles[7]
				p.Subscriptions = nil
				snapshot.AccountProfiles[7] = p
			}
			if tc.forbidden {
				snapshot.Channels[1].Settings["credit_pool_policy"] = "marketplace_universal_only"
			}
			if err := s.Reserve(ctx, req); !errors.Is(err, gateway.ErrInsufficientCredits) {
				t.Fatalf("reserve=%v", err)
			}
			for _, id := range []int64{42, 43} {
				if held, _ := rdb.HGet(ctx, BalanceKey(id), "reserved").Int64(); held != 0 {
					t.Fatalf("partial %d=%d", id, held)
				}
			}
		})
	}
}

func TestFundingPreferenceRedisSpecificBucketOrder(t *testing.T) {
	s, rdb, req, snapshot, clock := preferenceFixture(t, 1000, 1000, "subscription_only")
	profile := snapshot.AccountProfiles[7]
	profile.Subscriptions = append(profile.Subscriptions, catalog.SubscriptionBucket{SubscriptionID: 8, AccountID: 44, StartsAt: clock.now().Add(-time.Hour), ExpiresAt: clock.now().Add(2 * time.Hour)})
	profile.SubscriptionOrderIDs = []int64{999, 8, 9007199254740993}
	snapshot.AccountProfiles[7] = profile
	if err := rdb.HSet(ctx, BalanceKey(44), "balance", 450, "reserved", 0, "ver", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := s.Reserve(ctx, req); err != nil {
		t.Fatal(err)
	}
	h := req.Reserve.(*hold)
	if h.funding[0].account != 44 || h.funding[0].amount != 450 || h.funding[1].account != 43 || h.funding[1].amount != 450 {
		t.Fatalf("order=%+v", h.funding)
	}
	if err := s.Finalize(ctx, req, gateway.Outcome{Charge: true, Target: &req.Targets[0]}); err != nil {
		t.Fatal(err)
	}
	all := events(t, rdb)
	if len(all) != 2 || all[0]["subscription_id"] != "8" || all[1]["subscription_id"] != "9007199254740993" {
		t.Fatalf("event=%#v", all)
	}
}

func TestFundingPreferenceRedisConcurrentOnlyAdmission(t *testing.T) {
	s, rdb, _, snapshot, clock := preferenceFixture(t, 1000, 1800, "subscription_only")
	var wg sync.WaitGroup
	var admitted atomic.Int64
	for i := range 10 {
		wg.Go(func() {
			req, _ := sourceFixture(clock.now())
			req.ID = fmt.Sprintf("only-%d", i)
			err := s.Reserve(ctx, req)
			if errors.Is(err, gateway.ErrInsufficientCredits) {
				return
			}
			if err != nil {
				t.Error(err)
				return
			}
			admitted.Add(1)
			if err = s.Finalize(ctx, req, gateway.Outcome{Charge: true, Target: &req.Targets[0]}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if admitted.Load() != 2 {
		t.Fatalf("admitted=%d snapshot=%v", admitted.Load(), snapshot.AccountProfiles[7])
	}
	if b, h := balance(t, rdb); b != 1000 || h != 0 {
		t.Fatalf("wallet=%d/%d", b, h)
	}
	if b, _ := rdb.HGet(ctx, BalanceKey(43), "balance").Int64(); b != 0 {
		t.Fatalf("sub=%d", b)
	}
}
