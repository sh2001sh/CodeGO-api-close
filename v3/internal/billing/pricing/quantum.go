package pricing

import (
	"fmt"
	"math/big"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// MoneyQuantum preserves the migrated v2 rounding boundary: one old quota
// unit is two micro-credits. New native prices default to one micro-credit.
func MoneyQuantum(p catalog.Price) (int64, error) {
	value, exists := p.Rules["money_quantum"]
	if !exists {
		return 1, nil
	}
	n, err := catalogNumber(value)
	if err != nil || !n.IsInt() || !n.Num().IsInt64() || (n.Num().Int64() != 1 && n.Num().Int64() != 2) {
		return 0, fmt.Errorf("pricing: money_quantum must be 1 or 2")
	}
	return n.Num().Int64(), nil
}

func roundChargeQuantum(amount *big.Rat, quantum int64) (credits.Micro, error) {
	amount = new(big.Rat).Quo(amount, big.NewRat(quantum, 1))
	units, err := roundCharge(amount)
	if err != nil {
		return 0, err
	}
	n := new(big.Int).Mul(big.NewInt(int64(units)), big.NewInt(quantum))
	if !n.IsInt64() {
		return 0, credits.ErrOverflow
	}
	return credits.Micro(n.Int64()), nil
}
