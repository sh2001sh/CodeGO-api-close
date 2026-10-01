package commerce

import (
	"fmt"
	"math/big"
	"strings"
)

// Payment currencies determine their own integer minor units; credit grants
// remain micro-credits independently. Stablecoin price amounts retain v2's six
// decimal precision instead of silently rounding them to fiat cents.
func paymentScale(currency string) int64 {
	switch currency {
	case "bif", "clp", "djf", "gnf", "jpy", "kmf", "krw", "mga", "pyg", "rwf", "ugx", "vnd", "vuv", "xaf", "xof", "xpf":
		return 1
	case "bhd", "jod", "kwd", "omr", "tnd":
		return 1000
	case "usdt", "usdc":
		return 1_000_000
	default:
		return 100
	}
}

func formatCurrencyMinor(amount int64, currency string) string {
	scale := paymentScale(currency)
	if scale == 1 {
		return fmt.Sprintf("%d", amount)
	}
	digits := 2
	switch scale {
	case 1000:
		digits = 3
	case 1_000_000:
		digits = 6
	}
	return fmt.Sprintf("%d.%0*d", amount/scale, digits, amount%scale)
}

func parseCurrencyMinor(raw, currency string) (int64, error) {
	if raw == "" || len(raw) > 80 || strings.Count(raw, ".") > 1 || raw[0] == '.' || raw[len(raw)-1] == '.' {
		return 0, ErrInvalid
	}
	for _, ch := range raw {
		if ch != '.' && (ch < '0' || ch > '9') {
			return 0, ErrInvalid
		}
	}
	value, ok := new(big.Rat).SetString(raw)
	if !ok {
		return 0, ErrInvalid
	}
	value.Mul(value, big.NewRat(paymentScale(currency), 1))
	if !value.IsInt() || !value.Num().IsInt64() {
		return 0, ErrInvalid
	}
	return value.Num().Int64(), nil
}
