package commerce

import (
	"testing"
	"time"
)

func TestSubscriptionResetCalendarBoundaries(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	start := time.Date(2026, 9, 30, 23, 59, 0, 0, loc)
	end := start.AddDate(0, 2, 0)
	for _, tc := range []struct {
		rule    string
		seconds int64
		want    time.Time
	}{
		{"daily", 0, time.Date(2026, 10, 1, 0, 0, 0, 0, loc)},
		{"weekly", 0, start.AddDate(0, 0, 7)},
		{"monthly", 0, time.Date(2026, 10, 1, 0, 0, 0, 0, loc)},
		{"custom", 60, start.Add(time.Minute)},
	} {
		next := nextReset(start, tc.rule, tc.seconds, end)
		if next == nil || !next.Equal(tc.want) {
			t.Fatalf("%s next=%v want=%v", tc.rule, next, tc.want)
		}
	}
	if next := nextReset(start, "custom", 60, start.Add(time.Minute)); next != nil {
		t.Fatalf("reset at expiry: %v", next)
	}
	if validReset("custom", 0) || validReset("unknown", 60) {
		t.Fatal("invalid reset accepted")
	}
}

func TestSubscriptionPreferenceRejectsUnsupportedOrDuplicateSources(t *testing.T) {
	for _, p := range []SubscriptionPreference{{FundingSourceOrder: []string{"wallet", "wallet"}}, {FundingSourceOrder: []string{"unknown"}}, {SubscriptionOrderIDs: []int64{1, 1}}, {BillingPreference: "unsupported"}} {
		if _, err := normalizePreference(p); err == nil {
			t.Fatalf("accepted %+v", p)
		}
	}
}

func TestSubscriptionMonthlyDurationUsesCalendar(t *testing.T) {
	start := time.Date(2026, 10, 1, 4, 0, 0, 0, time.UTC)
	end := durationEnd(start, "month", 1, 0, 30*86400)
	if want := time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC); !end.Equal(want) {
		t.Fatalf("monthly expiry=%v want=%v", end, want)
	}
}
