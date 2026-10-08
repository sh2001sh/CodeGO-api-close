package marketplace

import (
	"math"
	"testing"

	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func TestBlindBatchValidationAndOverflow(t *testing.T) {
	b := Batch{Name: "finite", Purpose: "credits", Price: 100, BaseCredits: 100, Budget: 1000, ContributionSharePPM: 100000, Rewards: []BatchReward{{ID: "a", Title: "a", Kind: "credits", Amount: 10, Quantity: 2, Remaining: 2}}}
	if err := validateBatch(b); err != nil {
		t.Fatal(err)
	}
	if err := batchAmounts(&b); err != nil || b.RequiredBudget != 220 || b.TotalCount != 2 {
		t.Fatalf("totals %+v %v", b, err)
	}
	for _, mutate := range []func(*Batch){func(b *Batch) { b.BaseCredits = 99 }, func(b *Batch) { b.ContributionSharePPM = 100001 }, func(b *Batch) { b.Rewards[0].Kind = "tool" }, func(b *Batch) { b.Rewards[0].Quantity = 1000001 }} {
		v := b
		v.Rewards = append([]BatchReward(nil), b.Rewards...)
		mutate(&v)
		if validateBatch(v) == nil {
			t.Fatalf("accepted %+v", v)
		}
	}
	b.Rewards[0].Amount = credits.Micro(math.MaxInt64)
	if err := batchAmounts(&b); err != credits.ErrOverflow {
		t.Fatalf("overflow %v", err)
	}
}
