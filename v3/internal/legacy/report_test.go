package legacy

import (
	"math"
	"testing"
)

func TestValidateWalletCanonicalAndReviewCases(t *testing.T) {
	canonical := int64(500)
	for _, tc := range []struct {
		name   string
		wallet Wallet
		want   int64
		issues int
	}{
		{"projection only", Wallet{UserID: 1, ProjectionUnits: 500}, 1000, 0},
		{"canonical", Wallet{UserID: 1, ProjectionUnits: 500, SnapshotUnits: &canonical}, 1000, 0},
		{"mismatch", Wallet{UserID: 1, ProjectionUnits: 450, SnapshotUnits: &canonical}, 1000, 1},
		{"retired points excluded", Wallet{UserID: 1, ProjectionUnits: 500, GPTUnits: math.MaxInt64}, 1000, 0},
		{"reserved", Wallet{UserID: 1, ProjectionUnits: 500, ReservedUnits: 1}, 1000, 1},
		{"negative", Wallet{UserID: 1, ProjectionUnits: -1}, 0, 1},
		{"overflow", Wallet{UserID: 1, ProjectionUnits: math.MaxInt64}, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			amount, issues := ValidateWallet(tc.wallet)
			if int64(amount) != tc.want || len(issues) != tc.issues {
				t.Fatalf("amount=%d issues=%v", amount, issues)
			}
		})
	}
}

func TestOpeningReportAllowsAggregateAboveBigint(t *testing.T) {
	r := Report{}
	for range 2 {
		if err := r.addOpening(math.MaxInt64 - 1); err != nil {
			t.Fatal(err)
		}
	}
	if r.OpeningMicroCredits != "18446744073709551612" {
		t.Fatalf("aggregate precision lost: %s", r.OpeningMicroCredits)
	}
}
