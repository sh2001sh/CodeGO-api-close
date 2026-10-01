// Package credits defines the single balance unit used across v3.
//
// One credit equals what v2 displayed as $1. Amounts are stored as integer
// micro-credits so that pricing, reservation and settlement never touch floats.
package credits

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

// PerCredit is the number of micro-credits in one credit.
const PerCredit int64 = 1_000_000

// ErrOverflow reports that an arithmetic result does not fit in int64.
var ErrOverflow = errors.New("credits: overflow")

// Micro is an amount in micro-credits. Negative values represent debits.
type Micro int64

// FromCredits converts a whole number of credits to micro-credits.
func FromCredits(whole int64) (Micro, error) {
	if whole > math.MaxInt64/PerCredit || whole < math.MinInt64/PerCredit {
		return 0, ErrOverflow
	}
	return Micro(whole * PerCredit), nil
}

// Add returns a+b, or ErrOverflow if the sum does not fit in int64.
func (a Micro) Add(b Micro) (Micro, error) {
	sum := a + b
	if (b > 0 && sum < a) || (b < 0 && sum > a) {
		return 0, ErrOverflow
	}
	return sum, nil
}

// String formats the amount with two decimals, rounding half away from zero,
// for example "1234.56". Display layers add grouping and the "credits" label.
func (a Micro) String() string {
	const centsDivisor = PerCredit / 100
	value := int64(a)
	negative := value < 0
	var magnitude uint64
	if negative {
		magnitude = uint64(-(value + 1)) + 1 // safe for math.MinInt64
	} else {
		magnitude = uint64(value)
	}
	cents := (magnitude + uint64(centsDivisor)/2) / uint64(centsDivisor)

	var b strings.Builder
	if negative && cents != 0 {
		b.WriteByte('-')
	}
	b.WriteString(strconv.FormatUint(cents/100, 10))
	b.WriteByte('.')
	frac := cents % 100
	if frac < 10 {
		b.WriteByte('0')
	}
	b.WriteString(strconv.FormatUint(frac, 10))
	return b.String()
}
