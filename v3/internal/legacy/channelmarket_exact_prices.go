package legacy

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// Negotiated prices are live prices, not float noise. Numeric stores their
// exact scaled PPM, retaining the ordinary integral representation when possible.
func cmExactFactor(s string, allowZero bool) (any, error) {
	if s == "" && allowZero {
		return int64(0), nil
	}
	if len(s) > 1024 || !json.Valid([]byte(s)) {
		return nil, errors.New("invalid exact multiplier")
	}
	if at := strings.IndexAny(s, "eE"); at >= 0 {
		exponent, err := strconv.ParseInt(s[at+1:], 10, 32)
		if err != nil || exponent < -256 || exponent > 256 {
			return nil, errors.New("exact multiplier exponent exceeds supported range")
		}
	}
	value, valid := new(big.Rat).SetString(s)
	if !valid || value.Sign() < 0 || (!allowZero && value.Sign() == 0) {
		return nil, errors.New("invalid exact multiplier")
	}
	value.Mul(value, big.NewRat(1000000, 1))
	if value.Cmp(big.NewRat(math.MaxInt64, 1)) > 0 {
		return nil, errors.New("exact multiplier exceeds native range")
	}
	if value.IsInt() {
		return value.Num().Int64(), nil
	}
	precision, exact := value.FloatPrec()
	if !exact || precision > 1024 {
		return nil, errors.New("exact multiplier must be a bounded finite decimal")
	}
	return json.Number(value.FloatString(precision)), nil
}

func (b *cmBuilder) exactFactor(r cmRow, source, target string, allowZero bool) {
	value, err := cmExactFactor(r.text(source), allowZero)
	if err != nil && b.err == nil {
		b.err = fmt.Errorf("%s: %w", source, err)
	}
	b.put(target, value)
}

// Unlike the optional limits and old notices, a group's public price and a
// captured trend price must be present even when their legitimate value is zero.
func cmPublicFactor(s string) (int64, error) {
	if s == "" {
		return 0, errors.New("missing public multiplier")
	}
	return cmFactor(s, true)
}
