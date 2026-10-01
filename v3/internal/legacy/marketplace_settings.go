package legacy

import "math/big"

// v2 runtime Get() restores nonpositive prices/guarantees to their defaults.
// Preserve that effective behavior instead of storing a zero-price pool.
func marketplacePositiveSetting(value, fallback string) string {
	if n, ok := new(big.Rat).SetString(value); ok && n.Sign() <= 0 {
		return fallback
	}
	return value
}

func marketplaceProbabilitySetting(value string) string {
	n, ok := new(big.Rat).SetString(value)
	if !ok {
		return value
	}
	if n.Sign() < 0 {
		return "0"
	}
	if n.Cmp(big.NewRat(1, 1)) > 0 {
		return "1"
	}
	return value
}
