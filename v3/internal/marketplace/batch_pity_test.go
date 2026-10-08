package marketplace

import (
	"errors"
	"math"
	"testing"

	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func TestPaidRandomPityFloorsAndNaturalResets(t *testing.T) {
	p := batchPityPolicy(100)
	state := PityState{}
	for i := 1; i <= 50; i++ {
		extra, kind, err := advanceBatchPity(p, &state, BatchReward{Kind: "credits", Amount: 60})
		want, label := credits.Micro(0), "none"
		if i == 50 {
			want, label = 140, "big"
		} else if i%10 == 0 {
			want, label = 40, "small"
		}
		if err != nil || extra != want || kind != label {
			t.Fatalf("draw %d: extra=%d kind=%s state=%+v err=%v", i, extra, kind, state, err)
		}
	}
	if state != (PityState{Opened: 50}) {
		t.Fatalf("big did not reset both: %+v", state)
	}
	for _, tc := range []struct {
		reward BatchReward
		want   PityState
		extra  credits.Micro
		kind   string
	}{
		{BatchReward{Kind: "credits", Amount: 100}, PityState{Opened: 50, BigProgress: 49}, 0, "none"},
		{BatchReward{Kind: "credits", Amount: 200}, PityState{Opened: 50}, 0, "none"},
		{BatchReward{Kind: "subscription", Amount: 10000}, PityState{Opened: 50}, 200, "big"},
	} {
		state = PityState{Opened: 49, SmallProgress: 9, BigProgress: 49}
		// A natural small win still needs the large floor on its fiftieth draw.
		if tc.reward.Amount == 100 {
			state.BigProgress = 48
			tc.want.Opened = 50
		}
		extra, kind, err := advanceBatchPity(p, &state, tc.reward)
		if err != nil || state != tc.want || extra != tc.extra || kind != tc.kind {
			t.Fatalf("natural/subscription: %+v extra=%d/%d kind=%s/%s err=%v", state, extra, tc.extra, kind, tc.kind, err)
		}
	}
	state = PityState{Opened: math.MaxInt64}
	if _, _, err := advanceBatchPity(p, &state, BatchReward{Kind: "credits", Amount: 60}); !errors.Is(err, ErrConflict) {
		t.Fatal("opened overflow accepted")
	}
}

func TestPaidRandomDefaultWorstReserveAndUnchangedBasePool(t *testing.T) {
	b := Batch{Purpose: "paid_random", Price: 2_500_000, PityPolicy: batchPityPolicy(2_500_000)}
	amounts := []int64{1_500_000, 2_500_000, 3_000_000, 5_000_000, 10_000_000, 50_000_000, 100_000_000, 250_000_000}
	counts := []int64{3500, 5000, 1000, 400, 80, 15, 4, 1}
	for i, amount := range amounts {
		b.Rewards = append(b.Rewards, BatchReward{Kind: "credits", Amount: credits.Micro(amount), Quantity: counts[i], Remaining: counts[i]})
	}
	b.RemainingCount = 10000
	if err := batchAmounts(&b); err != nil || b.RequiredBudget != 51_700_000_000 {
		t.Fatalf("worst reserve %+v err=%v", b, err)
	}
	state := PityState{Opened: 49, SmallProgress: 9, BigProgress: 49}
	r, err := drawBatchRecord(&b, &state, func(int64) (int64, error) { return 0, nil })
	if err != nil || r.Reward.Amount != 1_500_000 || r.GuaranteeCredits != 3_500_000 || r.Guarantee != "big" || b.Rewards[0].Quantity != 3500 || b.Rewards[0].Remaining != 3499 {
		t.Fatalf("base prize changed by pity: %+v err=%v", r, err)
	}
	sub := Batch{Purpose: "paid_random", Price: 100, Rewards: []BatchReward{{Kind: "subscription", Amount: 500, Quantity: 2}}}
	if err := batchAmounts(&sub); err != nil || sub.RequiredBudget != 1400 {
		t.Fatalf("subscription needs full extra floor: %+v %v", sub, err)
	}
	sub.Rewards[0].Amount = credits.Micro(math.MaxInt64)
	if !errors.Is(batchAmounts(&sub), credits.ErrOverflow) {
		t.Fatal("subscription floor addition overflow accepted")
	}
}
