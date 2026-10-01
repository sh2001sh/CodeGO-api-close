//go:build pgintegration

package billing

import (
	"fmt"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestMarketPrimaryFreezesGrossAndActualFundingSource(t *testing.T) {
	for _, subscribed := range []int64{0, 600, 900} {
		t.Run(fmt.Sprint(subscribed), func(t *testing.T) {
			s, rdb, _, clock := setup(t, 1000)
			req, snap := targetPricingFixture()
			req.Targets = req.Targets[:1]
			for id, policy := range snap.Market.Channels {
				policy.CreditPolicy = "marketplace_subscription_and_universal"
				snap.Market.Channels[id] = policy
			}
			req.ID, req.Principal.UserID, req.Principal.KeyID = "market-source", 7, 70
			req.Principal.BudgetLimited, req.Principal.BudgetAccountID = true, 77
			profile := catalog.AccountProfile{WalletAccountID: account}
			if subscribed > 0 {
				profile.Subscriptions = []catalog.SubscriptionBucket{{AccountID: 43, StartsAt: clock.now().Add(-time.Hour), ExpiresAt: clock.now().Add(time.Hour)}}
				if err := rdb.HSet(ctx, BalanceKey(43), "balance", subscribed, "reserved", 0, "ver", 0, "base", 0).Err(); err != nil {
					t.Fatal(err)
				}
			}
			snap.AccountProfiles = map[int64]catalog.AccountProfile{7: profile}
			s.snapshot = func() *catalog.Snapshot { return snap }
			if err := s.Reserve(ctx, req); err != nil {
				t.Fatal(err)
			}
			// Neither changed wallet factor nor owner price may change settlement.
			snap.Market.Channels[1].ModelPrices["model"] = catalog.Price{Mode: "per_request", PerRequest: 9999}
			out := gateway.Outcome{Charge: true, Terminal: gateway.TerminalCompleted, Target: &req.Targets[0]}
			for range 2 {
				if err := s.Finalize(ctx, req, out); err != nil {
					t.Fatal(err)
				}
			}
			wantSource := "wallet"
			wantAmount := "90"
			if subscribed == 600 {
				wantSource = "mixed"
				wantAmount = "630"
			}
			if subscribed == 900 {
				wantSource = "subscription"
				wantAmount = "900"
			}
			primary := 0
			for _, event := range events(t, rdb) {
				if event["funding_part"] == "secondary" {
					continue
				}
				primary++
				if event[FieldMarketGross] != "90" || event[FieldMarketMultiplier] != "100000" || event[FieldBillingSource] != wantSource || event["usage_total_amount"] != wantAmount {
					t.Fatalf("market frozen facts/source lost or duplicated: %v", event)
				}
			}
			if primary != 1 {
				t.Fatalf("market primary count=%d", primary)
			}
		})
	}
}

func TestMarketRefundDoesNotAccrueIncome(t *testing.T) {
	s, rdb, _, _ := setup(t, 1000)
	req, snap := targetPricingFixture()
	req.ID, req.Principal.UserID = "market-refund", 7
	snap.AccountProfiles = map[int64]catalog.AccountProfile{7: {WalletAccountID: account}}
	s.snapshot = func() *catalog.Snapshot { return snap }
	if err := s.Reserve(ctx, req); err != nil {
		t.Fatal(err)
	}
	if err := s.Finalize(ctx, req, gateway.Outcome{Terminal: gateway.TerminalUpstreamErrorBeforeOutput, Target: &req.Targets[0]}); err != nil {
		t.Fatal(err)
	}
	all := events(t, rdb)
	if len(all) != 1 || all[0][FieldAmount] != "0" || all[0][FieldMarketGross] != nil {
		t.Fatalf("refund produced income facts: %v", all)
	}
}
