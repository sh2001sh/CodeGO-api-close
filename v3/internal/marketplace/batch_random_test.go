package marketplace

import (
	"errors"
	"math"
	"testing"

	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func TestPaidRandomBatchValidationAndPriceOverflow(t *testing.T) {
	b := Batch{Name: "random", Purpose: "paid_random", Price: 2_500_000, Budget: 10_000_000, ContributionSharePPM: 100000,
		Rewards: []BatchReward{{ID: "small", Title: "small", Kind: "credits", Amount: 1_200_000, Quantity: 2}}}
	if err := validateBatch(b); err != nil {
		t.Fatalf("random purchase without base credits rejected: %v", err)
	}
	if err := batchAmounts(&b); err != nil || b.RequiredBudget != 10_000_000 {
		t.Fatalf("random reserve includes unpromised base: %+v %v", b, err)
	}
	for _, change := range []func(*Batch){
		func(b *Batch) { b.Price = 0 },
		func(b *Batch) { b.BaseCredits = 1 },
		func(b *Batch) { b.Purpose = "credits" },
	} {
		invalid := b
		change(&invalid)
		if !errors.Is(validateBatch(invalid), ErrInvalidInput) {
			t.Fatalf("invalid price/base/purpose accepted: %+v", invalid)
		}
	}
	b.Price = credits.Micro(math.MaxInt64)
	if !errors.Is(batchAmounts(&b), credits.ErrOverflow) {
		t.Fatal("full-batch purchase amount overflow accepted")
	}
}
