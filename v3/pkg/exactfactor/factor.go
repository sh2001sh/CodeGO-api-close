// Package exactfactor preserves marketplace price factors in scaled PPM units.
// A value of 1000000 means a multiplier of one. Decimal text is the durable
// representation; conversion to a rounded monetary quantum belongs to billing.
package exactfactor

import (
	"errors"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

var ErrInvalid = errors.New("exactfactor: invalid nonnegative decimal PPM")

var decimalPattern = regexp.MustCompile(`^[+]?([0-9]+(\.[0-9]*)?|\.[0-9]+)([eE][+-]?[0-9]+)?$`)

// ParsePPM accepts decimal and scientific notation without passing through a
// float. Bounds match PostgreSQL's unconstrained numeric precision and scale.
func ParsePPM(raw string) (*big.Rat, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) == 0 || len(raw) > 200000 || !decimalPattern.MatchString(raw) {
		return nil, ErrInvalid
	}
	mantissa := raw
	exponent := 0
	if i := strings.IndexAny(raw, "eE"); i >= 0 {
		mantissa = raw[:i]
		var err error
		exponent, err = strconv.Atoi(raw[i+1:])
		if err != nil || exponent < -200000 || exponent > 200000 {
			return nil, ErrInvalid
		}
	}
	mantissa = strings.TrimPrefix(mantissa, "+")
	fractionDigits := 0
	if i := strings.IndexByte(mantissa, '.'); i >= 0 {
		fractionDigits = len(mantissa) - i - 1
		mantissa = mantissa[:i] + mantissa[i+1:]
	}
	mantissa = strings.TrimLeft(mantissa, "0")
	if mantissa == "" {
		return new(big.Rat), nil
	}
	// Trailing zeroes do not consume numeric scale.
	trailing := len(mantissa) - len(strings.TrimRight(mantissa, "0"))
	scale := fractionDigits - exponent - trailing
	integerDigits := len(mantissa) - fractionDigits + exponent
	if scale > 16383 || integerDigits > 131072 {
		return nil, ErrInvalid
	}
	r, ok := new(big.Rat).SetString(raw)
	if !ok || r.Sign() < 0 || r.Cmp(new(big.Rat).SetInt64(math.MaxInt64)) > 0 {
		return nil, ErrInvalid
	}
	return r, nil
}

func FromInt64(ppm int64) string { return strconv.FormatInt(ppm, 10) }

// Resolve validates and canonicalizes the authoritative exact value. Only an
// absent exact value falls back to the legacy integral PPM field.
func Resolve(ppm int64, exact string) (string, error) {
	if exact == "" {
		exact = FromInt64(ppm)
	}
	r, err := ParsePPM(exact)
	if err != nil {
		return "", err
	}
	return Decimal(r), nil
}

func Compare(a, b string) (int, error) {
	x, err := ParsePPM(a)
	if err != nil {
		return 0, err
	}
	y, err := ParsePPM(b)
	if err != nil {
		return 0, err
	}
	return x.Cmp(y), nil
}

// Multiplier converts scaled PPM to its exact human-facing multiplier.
func Multiplier(ppm string) (string, error) {
	r, err := ParsePPM(ppm)
	if err != nil {
		return "", err
	}
	r.Quo(r, big.NewRat(1000000, 1))
	return Decimal(r), nil
}

// Int64 succeeds only for an exactly integral value within the int64 range.
func Int64(ppm string) (int64, bool) {
	r, err := ParsePPM(ppm)
	if err != nil || !r.IsInt() || !r.Num().IsInt64() {
		return 0, false
	}
	return r.Num().Int64(), true
}

// Decimal returns the exact terminating decimal for values derived from decimal
// factors. For a nonterminating rational it returns exact rational notation,
// which ParsePPM rejects rather than silently rounding a factor.
func Decimal(r *big.Rat) string {
	if r == nil {
		return ""
	}
	den := new(big.Int).Set(r.Denom())
	twos := int(den.TrailingZeroBits())
	den.Rsh(den, uint(twos))
	fives := 0
	five, quotient, remainder := big.NewInt(5), new(big.Int), new(big.Int)
	for den.Cmp(big.NewInt(1)) != 0 {
		quotient.QuoRem(den, five, remainder)
		if remainder.Sign() != 0 {
			return r.RatString()
		}
		den.Set(quotient)
		fives++
	}
	scale := max(twos, fives)
	value := r.FloatString(scale)
	if scale > 0 {
		value = strings.TrimRight(strings.TrimRight(value, "0"), ".")
	}
	return value
}
