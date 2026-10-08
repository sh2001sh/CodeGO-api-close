package legacy

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
)

func (b *cmBuilder) settlementFactors(r cmRow) {
	walletValue, err := historicalSettlementFactor(r.text("multiplier"))
	if err != nil && b.err == nil {
		b.err = fmt.Errorf("multiplier: %w", err)
	}
	b.put("multiplier_ppm", walletValue)
	wallet, integral := walletValue.(int64)
	subscription, err := cmFactor(r.text("subscription_multiplier"), true)
	if err != nil && integral && wallet > 0 && wallet <= math.MaxInt64/10 {
		// Before V2 normalized factors, SubscriptionMultiplier persisted the
		// float64 result of wallet * 10. Recognize that exact operation only;
		// arbitrary nearby decimal values are not a rounding allowance.
		value, parseErr := strconv.ParseFloat(r.text("multiplier"), 64)
		original, valid := new(big.Rat).SetString(r.text("subscription_multiplier"))
		generated, generatedOK := new(big.Rat).SetString(strconv.FormatFloat(value*10, 'g', -1, 64))
		if parseErr == nil && valid && generatedOK && original.Cmp(generated) == 0 {
			subscription, err = wallet*10, nil
		}
	}
	if err != nil && b.err == nil {
		b.err = fmt.Errorf("subscription_multiplier: %w", err)
	}
	b.put("subscription_multiplier_ppm", subscription)
}

// Settlement factors are historical metadata, not a live routing price. V2
// allowed arbitrary decimal wallet multipliers; retain their exact scaled
// value in the native numeric column without recalculating any money.
func historicalSettlementFactor(s string) (any, error) {
	if value, err := cmFactor(s, false); err == nil {
		return value, nil
	}
	value, valid := new(big.Rat).SetString(s)
	if !json.Valid([]byte(s)) || !valid || value.Sign() <= 0 {
		return nil, errors.New("invalid historical multiplier")
	}
	value.Mul(value, big.NewRat(1000000, 1))
	if value.Cmp(big.NewRat(math.MaxInt64, 1)) > 0 {
		return nil, errors.New("historical multiplier exceeds native range")
	}
	precision, exact := value.FloatPrec()
	if !exact {
		return nil, errors.New("historical multiplier must be a finite decimal")
	}
	return json.Number(value.FloatString(precision)), nil
}
