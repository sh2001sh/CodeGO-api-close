package ledger

import (
	"errors"
	"math"
	"testing"

	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func TestArchiveMoneyKeepsDebitSignNullAndExactInteger(t *testing.T) {
	entry := HistoricalEntry{Direction: "debit"}
	if err := convertArchiveEntry(&entry, 9007199254740993, nil); err != nil || entry.Amount != -18014398509481986 || entry.BalanceAfter != nil {
		t.Fatalf("debit/null/exactness lost: %+v err=%v", entry, err)
	}
	balance := int64(-50)
	entry = HistoricalEntry{Direction: "credit"}
	if err := convertArchiveEntry(&entry, 25, &balance); err != nil || entry.Amount != 50 || entry.BalanceAfter == nil || *entry.BalanceAfter != -100 {
		t.Fatalf("credit/negative historical balance lost: %+v err=%v", entry, err)
	}
	for _, units := range []int64{math.MinInt64 / 2, math.MaxInt64 / 2} {
		if got, err := archiveMicro(units); err != nil || int64(got) != units*2 {
			t.Fatalf("valid boundary %d: %d %v", units, got, err)
		}
	}
	for _, units := range []int64{math.MinInt64/2 - 1, math.MaxInt64/2 + 1} {
		if _, err := archiveMicro(units); !errors.Is(err, credits.ErrOverflow) || !errors.Is(err, ErrHistoryArchive) {
			t.Fatalf("overflow boundary %d accepted: %v", units, err)
		}
	}
}

func TestArchiveRejectsCorruptFinancialEvidence(t *testing.T) {
	for _, test := range []struct {
		name      string
		direction string
		amount    int64
		balance   *int64
	}{
		{name: "negative source amount", direction: "debit", amount: -1},
		{name: "unknown direction", direction: "out", amount: 1},
		{name: "overflow amount", direction: "credit", amount: math.MaxInt64},
		{name: "overflow balance", direction: "credit", amount: 1, balance: func() *int64 { value := int64(math.MinInt64); return &value }()},
	} {
		t.Run(test.name, func(t *testing.T) {
			entry := HistoricalEntry{Direction: test.direction}
			if err := convertArchiveEntry(&entry, test.amount, test.balance); !errors.Is(err, ErrHistoryArchive) {
				t.Fatalf("invalid evidence accepted: %+v err=%v", entry, err)
			}
		})
	}
}
