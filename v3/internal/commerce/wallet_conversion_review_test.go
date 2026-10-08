package commerce

import (
	"errors"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func TestWalletFutureCreditsDoesNotRepeatCurrentOrExpiredGrants(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.FixedZone("HK", 8*3600))
	due := time.Date(2026, 10, 2, 0, 0, 0, 0, now.Location())
	f := walletConversionFacts{Total: 1000, Period: 300, Balance: 200, Used: 100, NextReset: &due, ResetPeriod: "daily", ExpiresAt: due.AddDate(0, 0, 3)}
	if got, err := walletFutureCredits(f, now); err != nil || got != 700 {
		t.Fatalf("lifetime-limited future=%d err=%v", got, err)
	}
	f.Total, f.Used = 0, 0
	if got, err := walletFutureCredits(f, now); err != nil || got != 900 {
		t.Fatalf("periodic future=%d err=%v", got, err)
	}
	f.Total, f.Period, f.Balance, f.Renewable, f.LegacyPeriodic = 1000, 0, 1000, 1000, true
	if got, err := walletFutureCredits(f, now); err != nil || got != 3000 {
		t.Fatalf("legacy renewable future=%d err=%v", got, err)
	}
	if _, err := walletFutureCredits(f, due); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("due cycle must be settled first: %v", err)
	}
}

func TestWalletReviewRejectsOmittedFutureAndInventedPrincipal(t *testing.T) {
	revenue := credits.Micro(100)
	f := walletConversionFacts{Balance: 60, Sources: []WalletConversionSource{{OrderID: 1, State: "paid", Credits: 100, RevenueCredits: &revenue}}}
	segments := []WalletConversionSegment{{Name: "paid", OriginalOrderID: 1, SourceTotal: 100, CurrentCredits: 60, WalletCredits: 100, PaidWalletCredits: 100}}
	if err := valueWalletConversionSegments(segments, f, 0); err != nil || segments[0].PaidCredits != 60 || segments[0].RevenueMultiplierPPM != 1000000 {
		t.Fatalf("paid valuation=%+v err=%v", segments, err)
	}
	if err := valueWalletConversionSegments(segments, f, 40); !errors.Is(err, ErrInvalid) {
		t.Fatalf("future promise omitted: %v", err)
	}
	segments[0].FutureCredits = 40
	if err := valueWalletConversionSegments(segments, f, 40); err != nil {
		t.Fatal(err)
	}
	f.Sources[0].State = "refunded"
	if err := valueWalletConversionSegments(segments, f, 40); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("refunded money became principal: %v", err)
	}
	f.Sources[0].State = "paid"
	segments[0].OriginalOrderID = 0
	if err := valueWalletConversionSegments(segments, f, 40); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("unpaid reward became principal: %v", err)
	}
	segments[0].PaidWalletCredits = 0
	if err := valueWalletConversionSegments(segments, f, 40); err != nil || segments[0].PaidCredits != 0 {
		t.Fatalf("genuine reward valuation=%+v err=%v", segments, err)
	}
}
