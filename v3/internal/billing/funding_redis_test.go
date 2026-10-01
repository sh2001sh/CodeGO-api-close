//go:build pgintegration

package billing

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type fundedResolver struct {
	fixedAccounts
	sources []int64
}

func (a fundedResolver) SubscriptionAccounts(context.Context, int64) ([]int64, error) {
	return a.sources, nil
}

func TestFundingSplitsSubscriptionFirstAndRefundsUnusedEstimate(t *testing.T) {
	s, rdb, _, _ := setup(t, 1000)
	s.accounts = fundedResolver{sources: []int64{43}}
	if err := rdb.HSet(ctx, BalanceKey(43), "balance", 100, "reserved", 0, "ver", 0, "base", 0).Err(); err != nil {
		t.Fatal(err)
	}
	request := newReq("split")
	if err := s.Reserve(ctx, request); err != nil {
		t.Fatal(err)
	}
	if _, held := balance(t, rdb); held != 108 {
		t.Fatalf("wallet held=%d", held)
	}
	if held, _ := rdb.HGet(ctx, BalanceKey(43), "reserved").Int64(); held != 100 {
		t.Fatalf("subscription held=%d", held)
	}
	if err := s.Finalize(ctx, request, completed(10, 20)); err != nil {
		t.Fatal(err)
	} // actual 50
	if bal, held := balance(t, rdb); bal != 1000 || held != 0 {
		t.Fatalf("wallet charged before subscription: %d %d", bal, held)
	}
	if bal, _ := rdb.HGet(ctx, BalanceKey(43), "balance").Int64(); bal != 50 {
		t.Fatalf("subscription balance=%d", bal)
	}
	if err := s.Finalize(ctx, request, completed(10, 20)); err != nil {
		t.Fatal(err)
	}
	if len(events(t, rdb)) != 1 {
		t.Fatal("duplicate funding finalize emitted a charge")
	}
	if err := s.Reserve(ctx, request); err == nil {
		t.Fatal("finalized funding request re-admitted")
	}
}

func TestFundingInsufficientIsAtomicAndActualBeyondEstimateHitsWallet(t *testing.T) {
	s, rdb, _, _ := setup(t, 50)
	s.accounts = fundedResolver{sources: []int64{43}}
	if err := rdb.HSet(ctx, BalanceKey(43), "balance", 100, "reserved", 0, "ver", 0, "base", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := s.Reserve(ctx, newReq("insufficient")); !errors.Is(err, gateway.ErrInsufficientCredits) {
		t.Fatalf("reserve %v", err)
	}
	if held, _ := rdb.HGet(ctx, BalanceKey(43), "reserved").Int64(); held != 0 {
		t.Fatal("failed admission left subscription held")
	}
	if err := rdb.HSet(ctx, BalanceKey(account), "balance", 1000).Err(); err != nil {
		t.Fatal(err)
	}
	request := newReq("overestimate")
	if err := s.Reserve(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := s.Finalize(ctx, request, completed(10, 120)); err != nil {
		t.Fatal(err)
	} // actual 250
	if bal, held := balance(t, rdb); bal != 850 || held != 0 {
		t.Fatalf("wallet=%d held=%d", bal, held)
	}
	if bal, _ := rdb.HGet(ctx, BalanceKey(43), "balance").Int64(); bal != 0 {
		t.Fatalf("subscription=%d", bal)
	}
	event := events(t, rdb)
	if len(event) != 2 || event[0][FieldAmount] != "100" || event[1][FieldAmount] != "150" {
		t.Fatalf("funding events %+v", event)
	}
}

func TestFundingBudgetVersionOverflowDoesNotPartiallyChargeWallet(t *testing.T) {
	s, rdb, _, _ := setup(t, 1000)
	if err := rdb.HSet(ctx, BalanceKey(43), "balance", 1000, "reserved", 0, "ver", int64(math.MaxInt64), "base", 0).Err(); err != nil {
		t.Fatal(err)
	}
	request := newReq("overflow")
	request.Principal.BudgetLimited, request.Principal.BudgetAccountID = true, 43
	if err := s.Reserve(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := s.Finalize(ctx, request, completed(10, 20)); err == nil {
		t.Fatal("version overflow silently applied")
	}
	if bal, held := balance(t, rdb); bal != 1000 || held != 208 {
		t.Fatalf("wallet partially mutated before budget failure: %d %d", bal, held)
	}
	if len(events(t, rdb)) != 0 {
		t.Fatal("failed settlement emitted a partial charge")
	}
	if err := rdb.HSet(ctx, BalanceKey(43), "ver", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := s.Finalize(ctx, request, completed(10, 120)); err != nil {
		t.Fatal(err)
	}
	if bal, held := balance(t, rdb); bal != 750 || held != 0 {
		t.Fatalf("retry did not apply exact full charge: %d %d", bal, held)
	}
	if bal, _ := rdb.HGet(ctx, BalanceKey(43), "balance").Int64(); bal != 750 {
		t.Fatalf("key budget failed to charge actual beyond estimate: %d", bal)
	}
}
