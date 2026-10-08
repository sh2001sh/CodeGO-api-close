//go:build pgintegration

package marketplace

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
)

func randomBatch(t *testing.T, f *fixture, count int64) Batch {
	t.Helper()
	return seedBatch(t, f, "paid_random", []BatchReward{{ID: "low", Title: "low", Kind: "credits", Amount: 60, Quantity: count}})
}

func TestPaidRandomAndGuaranteedCreditsShareDailyTenBothDirections(t *testing.T) {
	for _, randomFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("random-first-%t", randomFirst), func(t *testing.T) {
			f := newFixture(t)
			random := randomBatch(t, f, 20)
			guaranteed := seedBatch(t, f, "credits", []BatchReward{{ID: "legacy", Title: "legacy", Kind: "credits", Amount: 1, Quantity: 20}})
			first, second := guaranteed, random
			if randomFirst {
				first, second = random, guaranteed
			}
			initial, err := f.s.DrawBatch(testContext, 2, first.ID, "first-five", 5)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.s.DrawBatch(testContext, 2, second.ID, "second-five", 5); err != nil {
				t.Fatal(err)
			}
			for _, b := range []Batch{random, guaranteed} {
				if _, err = f.s.DrawBatch(testContext, 2, b.ID, fmt.Sprintf("extra-%d", b.ID), 1); !errors.Is(err, ErrDailyLimit) {
					t.Fatalf("priced batch %s exceeded daily ten: %v", b.Purpose, err)
				}
			}
			replay, err := f.s.DrawBatch(testContext, 2, first.ID, "first-five", 5)
			if err != nil || replay.Records[0].ID != initial.Records[0].ID {
				t.Fatalf("priced replay blocked: %+v %v", replay, err)
			}
			overview, err := f.s.BatchOverview(testContext, 2)
			if err != nil || overview.DailyPurchased != 10 || overview.Pity.Opened != 5 {
				t.Fatalf("old credits changed new pity or escaped limit: %+v %v", overview, err)
			}
		})
	}
}

func TestPaidRandomDailyTenAcrossBatchesLegacyAndShanghaiMidnight(t *testing.T) {
	f := newFixture(t)
	f.now.Store(time.Date(2026, 9, 30, 15, 59, 59, 0, time.UTC).Unix())
	a, b := randomBatch(t, f, 20), randomBatch(t, f, 20)
	first, err := f.s.DrawBatch(testContext, 2, a.ID, "six", 6)
	if err != nil || first.Pity.Opened != 6 {
		t.Fatalf("first: %+v %v", first, err)
	}
	last, err := f.s.DrawBatch(testContext, 2, b.ID, "four", 4)
	if err != nil || last.Records[3].GuaranteeCredits != 40 || last.Records[3].Guarantee != "small" {
		t.Fatalf("cross-batch small floor: %+v %v", last, err)
	}
	if _, err = f.s.DrawBatch(testContext, 2, a.ID, "eleven", 1); !errors.Is(err, ErrDailyLimit) {
		t.Fatalf("eleventh: %v", err)
	}
	replay, err := f.s.DrawBatch(testContext, 2, a.ID, "six", 6)
	if err != nil || replay.Records[0].ID != first.Records[0].ID {
		t.Fatalf("limit blocked replay: %+v %v", replay, err)
	}
	if _, err = f.s.DrawBatch(testContext, 2, a.ID, "six", 5); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed replay: %v", err)
	}
	p := f.seedPool(t, Reward{Kind: "credits", Amount: 1})
	if _, err = f.s.PurchaseBoxes(testContext, 2, "legacy-after-ten", p.ID, 1); !errors.Is(err, ErrDailyLimit) {
		t.Fatalf("legacy wallet bypass: %v", err)
	}
	err = pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		_, err := f.s.CreateBoxOrderTx(testContext, tx, BoxOrderInput{UserID: 2, PoolID: p.ID, Quantity: 1, AmountMinor: 250, TradeNo: "cash-after-ten", Currency: "cny", PaymentProvider: "epay"})
		return err
	})
	if !errors.Is(err, ErrDailyLimit) {
		t.Fatalf("legacy cash bypass: %v", err)
	}
	overview, err := f.s.BatchOverview(testContext, 2)
	if err != nil || overview.DailyPurchaseLimit != 10 || overview.DailyPurchased != 10 || overview.Pity.Opened != 10 {
		t.Fatalf("overview: %+v %v", overview, err)
	}
	f.now.Add(1)
	next, err := f.s.DrawBatch(testContext, 2, a.ID, "midnight", 1)
	if err != nil || next.Pity != (PityState{Opened: 11, SmallProgress: 1, BigProgress: 11}) {
		t.Fatalf("day reset lost global pity: %+v %v", next, err)
	}
	overview, err = f.s.BatchOverview(testContext, 2)
	if err != nil || overview.DailyPurchased != 1 {
		t.Fatalf("Shanghai midnight: %+v %v", overview, err)
	}
}

func TestPaidRandomLegacyPurchasesCountOnceWithoutOpenDoubleCount(t *testing.T) {
	f := newFixture(t)
	b := randomBatch(t, f, 20)
	p := f.seedPool(t, Reward{Kind: "credits", Amount: 1})
	var o BoxOrder
	err := pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		var err error
		o, err = f.s.CreateBoxOrderTx(testContext, tx, BoxOrderInput{UserID: 2, PoolID: p.ID, Quantity: 3, AmountMinor: 750, TradeNo: "cash-three", Currency: "cny", PaymentProvider: "epay"})
		if err != nil {
			return err
		}
		_, err = f.s.CompleteBoxOrderTx(testContext, tx, 2, o.TradeNo, o.AmountMinor, "cny")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.PurchaseBoxes(testContext, 2, "wallet-two", p.ID, 2); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.OpenBoxes(testContext, 2, "legacy-open", 5); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.DrawBatch(testContext, 2, b.ID, "new-five", 5); err != nil {
		t.Fatal(err)
	}
	overview, err := f.s.BatchOverview(testContext, 2)
	if err != nil || overview.DailyPurchased != 10 {
		t.Fatalf("external inventory or legacy opens counted twice: %+v %v", overview, err)
	}
	if _, err = f.s.DrawBatch(testContext, 2, b.ID, "extra", 1); !errors.Is(err, ErrDailyLimit) {
		t.Fatalf("legacy purchase count excluded: %v", err)
	}
}

func TestPaidRandomConcurrentDailyTenSharedUserLock(t *testing.T) {
	f := newFixture(t)
	batches := []Batch{randomBatch(t, f, 20), randomBatch(t, f, 20)}
	var wg sync.WaitGroup
	results := make(chan error, 20)
	for i := range 20 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := f.s.DrawBatch(testContext, 2, batches[i%2].ID, fmt.Sprintf("parallel-%d", i), 1)
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		} else if !errors.Is(err, ErrDailyLimit) {
			t.Fatal(err)
		}
	}
	if wins != 10 || f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_open_records WHERE user_id=2 AND batch_id IS NOT NULL`) != 10 {
		t.Fatalf("daily admissions=%d", wins)
	}
	overview, err := f.s.BatchOverview(testContext, 2)
	if err != nil || overview.Pity.Opened != 10 || overview.DailyPurchased != 10 {
		t.Fatalf("concurrent progress %+v %v", overview, err)
	}
}

type failBatchGuarantee struct{ base Poster }

func (p failBatchGuarantee) PostTx(ctx context.Context, tx pgx.Tx, e billing.Entry) (billing.PostResult, error) {
	if strings.HasSuffix(e.OperationID, ":guarantee") {
		return billing.PostResult{}, errors.New("injected guarantee failure")
	}
	return p.base.PostTx(ctx, tx, e)
}

func TestPaidRandomBigFloorRollbackHistoryStatisticsAndAPISource(t *testing.T) {
	f := newFixture(t)
	b := randomBatch(t, f, 20)
	if _, err := f.pool.Exec(testContext, `INSERT INTO v3_marketplace.blind_box_batch_pity(user_id,opened,small_progress,big_progress) VALUES(2,49,9,49)`); err != nil {
		t.Fatal(err)
	}
	f.s.money = failBatchGuarantee{base: f.poster}
	if _, err := f.s.DrawBatch(testContext, 2, b.ID, "big", 1); err == nil {
		t.Fatal("supplement failure committed")
	}
	if f.balance(t, 2) != 10000 || f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_open_records WHERE user_id=2`) != 0 || f.count(t, `SELECT opened FROM v3_marketplace.blind_box_batch_pity WHERE user_id=2`) != 49 || f.count(t, `SELECT spent_budget_micro FROM v3_marketplace.blind_box_batches WHERE id=$1`, b.ID) != 0 {
		t.Fatal("failed guarantee left partial money/record/progress/budget")
	}
	f.s.money = f.poster
	out, err := f.s.DrawBatch(testContext, 2, b.ID, "big", 1)
	if err != nil || out.Pity != (PityState{Opened: 50}) || out.Records[0].Reward.Amount != 60 || out.Records[0].GuaranteeCredits != 140 || out.Records[0].Guarantee != "big" || f.balance(t, 2) != 10100 {
		t.Fatalf("big floor: %+v %v", out, err)
	}
	history, err := f.s.History(testContext, 2, 0, 10)
	if err != nil || len(history) != 1 || history[0].GuaranteeCredits != 140 || history[0].Reward.Amount != 60 {
		t.Fatalf("history: %+v %v", history, err)
	}
	stats, err := f.s.BatchStats(testContext, b.ID)
	if err != nil || stats.RewardCredits != 200 || stats.Spent != 200 || stats.Remaining != 3800 {
		t.Fatalf("statistics omit guarantee %+v %v", stats, err)
	}
	if f.count(t, `SELECT count(*) FROM v3_billing.funding_lots WHERE source='blind_box_batch_reward' AND original_amount=140 AND non_transferable AND non_refundable AND revenue_multiplier_ppm=0`) != 1 {
		t.Fatal("supplement escaped API-only funding")
	}
	wallet, err := f.accounts.WalletAccount(testContext, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.poster.Post(testContext, billing.Entry{AccountID: wallet, Amount: -9900, Kind: "transfer", OperationID: "remove-paid"}); err != nil {
		t.Fatal(err)
	}
	// Only the two prize lots remain; neither can pay for another product.
	if _, err = f.poster.Post(testContext, billing.Entry{AccountID: wallet, Amount: -200, Kind: "transfer", OperationID: "spend-paid"}); err == nil {
		t.Fatal("API-only supplement used as product funds")
	}
	if _, err = f.s.DrawBatch(testContext, 2, b.ID, "reward-rebuy", 1); !errors.Is(err, ledger.ErrWalletAPICreditsPurchaseLocked) {
		t.Fatalf("supplement funded next box: %v", err)
	}
	if _, err = f.poster.Post(testContext, billing.Entry{AccountID: wallet, Amount: -50, Kind: "usage", OperationID: "consume-api-reward"}); err != nil || f.balance(t, 2) != 150 {
		t.Fatalf("API reward could not be used: %v", err)
	}
}
