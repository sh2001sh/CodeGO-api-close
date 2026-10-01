package billing

import (
	"fmt"
	"strconv"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

const (
	FieldMarketGross      = "marketplace_gross_micro"
	FieldMarketMultiplier = "marketplace_multiplier_ppm"
	FieldBillingSource    = "billing_source"
)

// The selected admission price is a wallet-equivalent price. Preserve it in
// the same WAL/event as payment so income never guesses gross from a funding
// bucket or a subsequently published catalog. Lua identifies actual sources.
func (s *Settler) appendMarketCall(rec walRecord, h *hold, out gateway.Outcome, actual credits.Micro) (walRecord, error) {
	if !out.Charge || (actual == 0 && !h.sourceMode) || out.Target == nil || len(h.targetPrices) == 0 {
		return rec, nil
	}
	price, ok := h.targetPrices[targetPriceKey(*out.Target)]
	if !ok {
		return rec, fmt.Errorf("billing: market settlement target was not admitted")
	}
	if !price.Market {
		return rec, nil
	}
	if price.MultiplierPPM < 0 {
		return rec, fmt.Errorf("billing: negative frozen market multiplier")
	}
	rec.Args = append(rec.Args, FieldMarketGross, strconv.FormatInt(int64(actual), 10),
		FieldMarketMultiplier, strconv.FormatInt(price.MultiplierPPM, 10), FieldBillingSource, "wallet")
	return rec, nil
}
