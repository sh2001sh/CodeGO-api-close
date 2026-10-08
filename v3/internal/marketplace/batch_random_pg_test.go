//go:build pgintegration

package marketplace

import (
	"errors"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
)

func TestPaidRandomBatchBelowPriceReplayAndNoRewardRepurchase(t *testing.T) {
	f := newFixture(t)
	b := seedBatch(t, f, "paid_random", []BatchReward{
		{ID: "small", Title: "small", Kind: "credits", Amount: 60, Quantity: 1},
		{ID: "jackpot", Title: "jackpot", Kind: "credits", Amount: 500, Quantity: 1},
	})
	if b.RequiredBudget != 700 || f.balance(t, 1) != 9300 {
		t.Fatalf("incorrect full reserve: %+v", b)
	}
	wallet, err := f.accounts.WalletAccount(testContext, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.poster.Post(testContext, billing.Entry{AccountID: wallet, Amount: -9900, Kind: "usage", OperationID: "clear-paid-funds"}); err != nil {
		t.Fatal(err)
	}
	result, err := f.s.DrawBatch(testContext, 2, b.ID, "paid-random", 1)
	if err != nil || result.Charged != 100 || result.BaseCredits != 0 || result.Records[0].Reward.Amount != 60 || f.balance(t, 2) != 60 {
		t.Fatalf("below-price prize: %+v %v", result, err)
	}
	replay, err := f.s.DrawBatch(testContext, 2, b.ID, "paid-random", 1)
	if err != nil || replay.Records[0].ID != result.Records[0].ID || f.balance(t, 2) != 60 {
		t.Fatalf("duplicate purchase: %+v %v", replay, err)
	}
	// More than the purchase price in restricted rewards is still not spendable on another box.
	if _, err = f.poster.Post(testContext, billing.Entry{AccountID: wallet, Amount: 500, Kind: "reward", Reason: "blind_box_batch_reward", OperationID: "extra-prize"}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.DrawBatch(testContext, 2, b.ID, "snowball", 1); !errors.Is(err, ledger.ErrWalletAPICreditsPurchaseLocked) {
		t.Fatalf("reward-funded box purchase returned wrong result: %v", err)
	}
	if f.balance(t, 2) != 560 || f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_open_records WHERE batch_id=$1`, b.ID) != 1 {
		t.Fatal("rejected purchase changed money or stock")
	}
	paused, err := f.s.ChangeBatchState(testContext, 1, b.ID, b.Revision, "pause", false)
	if err != nil || paused.RemainingCount != 1 || paused.RemainingBudget != 640 {
		t.Fatalf("remaining jackpot reserve: %+v %v", paused, err)
	}
	if _, err = f.s.DrawBatch(testContext, 3, b.ID, "paused-sale", 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("paused random batch sold: %v", err)
	}
	// The old guaranteed-credit promise remains enforced at the database boundary.
	if _, err = f.pool.Exec(testContext, `UPDATE v3_marketplace.blind_box_batches SET purpose='credits' WHERE id=$1`, b.ID); err == nil {
		t.Fatal("database accepted guaranteed credits without guaranteed base")
	}
}
