//go:build pgintegration

package billing

import (
	"strconv"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

// Regression: the reservation, actual wallet debit, separate API-key budget,
// and emitted ledger amount all use the actual target's frozen owner price.
func TestTargetRedisReserveMaximumSettleActual(t *testing.T) {
	for _, limited := range []bool{false, true} {
		t.Run(strconv.FormatBool(limited), func(t *testing.T) {
			s, rdb, _, _ := setup(t, 1000)
			req, snapshot := targetPricingFixture()
			req.ID = "target-redis"
			req.Principal.UserID, req.Principal.KeyID = 7, 70
			req.Principal.BudgetLimited, req.Principal.BudgetAccountID = limited, 77
			snapshot.Prices = nil
			snapshot.AccountProfiles = map[int64]catalog.AccountProfile{7: {WalletAccountID: account}}
			s.snapshot = func() *catalog.Snapshot { return snapshot }
			if err := s.Reserve(ctx, req); err != nil {
				t.Fatal(err)
			}
			if bal, held := balance(t, rdb); bal != 1000 || held != 300 {
				t.Fatalf("reserve balance=%d held=%d; want 1000/300", bal, held)
			}
			// Catalog publication rotates after admission, before completion.
			snapshot.Market.Channels[1].ModelPrices["model"] = catalog.Price{Mode: "per_request", PerRequest: 5000}
			out := gateway.Outcome{Charge: true, Delivered: true, Terminal: gateway.TerminalCompleted, Target: &req.Targets[0]}
			if err := s.Finalize(ctx, req, out); err != nil {
				t.Fatal(err)
			}
			if err := s.Finalize(ctx, req, out); err != nil {
				t.Fatal(err)
			}
			if bal, held := balance(t, rdb); bal != 910 || held != 0 {
				t.Fatalf("actual fallback balance=%d held=%d; want 910/0", bal, held)
			}
			if limited {
				if value, err := rdb.HGet(ctx, BalanceKey(77), "balance").Int64(); err != nil || value != 910 {
					t.Fatalf("key budget actual = %d, %v; want 910", value, err)
				}
			}
			stream := events(t, rdb)
			wantEvents := 1
			if limited {
				wantEvents = 2 // wallet funding and the independent API-key limit
			}
			if len(stream) != wantEvents || stream[0][FieldAmount] != "90" || stream[0][FieldChannelID] != "1" {
				t.Fatalf("actual ledger event = %#v", stream)
			}
			if stream[0][FieldMarketGross] != "90" || stream[0][FieldMarketMultiplier] != "100000" || stream[0][FieldBillingSource] != "wallet" {
				t.Fatalf("actual market ledger facts = %#v", stream[0])
			}
		})
	}
}
