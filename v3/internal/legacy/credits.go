// Package legacy is the only v3 package allowed to understand v2 data.
// It is deleted together with v2 in milestone M6.
package legacy

import (
	"errors"
	"math"

	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// V2UnitsPerCredit is v2's QuotaPerUnit: 500,000 internal quota units were
// displayed as $1, which v3 calls 1 credit.
const V2UnitsPerCredit int64 = 500_000

// microPerV2Unit is exact: 1,000,000 / 500,000.
const microPerV2Unit = credits.PerCredit / V2UnitsPerCredit

// ErrNegativeBalance reports a negative v2 balance, which must be reviewed by
// hand instead of being migrated silently.
var ErrNegativeBalance = errors.New("legacy: negative v2 balance")

// FromV2Units converts any v2 amount stored in internal quota units (unified
// wallet, token remain_quota, subscription quota, marketplace earnings, log
// quota) to micro-credits. The conversion is exact; no rounding happens.
//
// The one-time GPT-to-unified ratio (1/4, or 11/100 for one account) was
// already applied inside v2 and must not be applied again here.
func FromV2Units(units int64) (credits.Micro, error) {
	if units > math.MaxInt64/microPerV2Unit || units < math.MinInt64/microPerV2Unit {
		return 0, credits.ErrOverflow
	}
	return credits.Micro(units * microPerV2Unit), nil
}

// OpeningBalance converts a v2 wallet balance for the v3 opening ledger entry.
// Negative balances are rejected so the migration report can list them.
func OpeningBalance(units int64) (credits.Micro, error) {
	if units < 0 {
		return 0, ErrNegativeBalance
	}
	return FromV2Units(units)
}
