package legacy

import (
	"fmt"
	"math/big"

	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// Issue identifies a row that needs correction before an offline import.
// Reports contain IDs and amounts, never passwords or provider credentials.
type Issue struct {
	Entity string `json:"entity"`
	ID     int64  `json:"id"`
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

type Report struct {
	Applied              bool                  `json:"applied"`
	Users                int                   `json:"users"`
	Keys                 int                   `json:"api_keys"`
	Channels             int                   `json:"channels"`
	UnmappedSources      []string              `json:"unmapped_sources"`
	Counts               map[string]int64      `json:"counts"`
	Amounts              map[string]string     `json:"amounts"`
	OpeningMicroCredits  string                `json:"opening_micro_credits"`
	Issues               []Issue               `json:"issues"`
	LedgerHistoryArchive *LedgerHistoryArchive `json:"ledger_history_archive,omitempty"`
}

type Wallet struct {
	UserID          int64
	GPTUnits        int64
	ProjectionUnits int64
	SnapshotUnits   *int64
	ReservedUnits   int64
}

// ValidateWallet uses the canonical snapshot when one exists and blocks
// inconsistent projections and in-flight reservations. Retired GPT points are
// reported separately and never added to the current monetary wallet.
func ValidateWallet(w Wallet) (credits.Micro, []Issue) {
	var issues []Issue
	add := func(code, detail string) { issues = append(issues, Issue{"user", w.UserID, code, detail}) }
	units := w.ProjectionUnits
	if w.SnapshotUnits != nil {
		units = *w.SnapshotUnits
		if units != w.ProjectionUnits {
			add("wallet_projection_mismatch", fmt.Sprintf("snapshot=%d projection=%d v2 units", units, w.ProjectionUnits))
		}
	}
	if w.ReservedUnits != 0 {
		add("open_reservations", fmt.Sprintf("reserved=%d v2 units", w.ReservedUnits))
	}
	amount, err := OpeningBalance(units)
	if err != nil {
		add("invalid_wallet_amount", err.Error())
	}
	return amount, issues
}

func (r *Report) addOpening(amount credits.Micro) error {
	total := new(big.Int)
	if r.OpeningMicroCredits != "" {
		if _, ok := total.SetString(r.OpeningMicroCredits, 10); !ok {
			return fmt.Errorf("legacy: invalid opening total")
		}
	}
	total.Add(total, big.NewInt(int64(amount)))
	r.OpeningMicroCredits = total.String()
	return nil
}
