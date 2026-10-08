package billing

import (
	"fmt"
	"math/big"
	"strconv"

	"github.com/sh2001sh/new-api/v3/internal/billing/pricing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

const (
	FieldRoutePoolID     = "route_pool_id"
	FieldProcurementCost = "procurement_cost_multiplier_ppm"
)

func freezeProcurement(target gateway.Target) (int64, int64, error) {
	if target.RoutePoolID == 0 && target.ProcurementCostMultiplier == "" {
		return 0, 0, nil
	}
	if target.RoutePoolID <= 0 || !catalog.PositivePoolDecimal(target.ProcurementCostMultiplier) {
		return 0, 0, fmt.Errorf("billing: invalid selected procurement facts")
	}
	rate, ok := new(big.Rat).SetString(target.ProcurementCostMultiplier)
	if !ok {
		return 0, 0, fmt.Errorf("billing: invalid procurement multiplier")
	}
	rate.Mul(rate, big.NewRat(1_000_000, 1))
	numerator := new(big.Int).Lsh(new(big.Int).Set(rate.Num()), 1)
	numerator.Add(numerator, rate.Denom())
	denominator := new(big.Int).Lsh(new(big.Int).Set(rate.Denom()), 1)
	ppm := numerator.Quo(numerator, denominator)
	if !ppm.IsInt64() {
		return 0, 0, credits.ErrOverflow
	}
	return target.RoutePoolID, ppm.Int64(), nil
}

func appendEconomicsCall(rec walRecord, h *hold, out gateway.Outcome) (walRecord, error) {
	if !out.Charge || out.Target == nil || len(h.targetPrices) == 0 {
		return rec, nil
	}
	price, found := h.targetPrices[targetPriceKey(*out.Target)]
	if !found || price.RoutePoolID < 0 || price.ProcurementCostMultiplierPPM < 0 {
		return rec, fmt.Errorf("billing: invalid admitted procurement target")
	}
	if price.RoutePoolID > 0 {
		rec.Args = append(rec.Args, FieldRoutePoolID, strconv.FormatInt(price.RoutePoolID, 10),
			FieldProcurementCost, strconv.FormatInt(price.ProcurementCostMultiplierPPM, 10))
		cost, err := pricing.PriceForRequestPPM(out.Usage, price.Price, price.ProcurementCostMultiplierPPM, h.pricingInput)
		if err != nil {
			return rec, err
		}
		value, err := pricing.PriceForRequestPPM(out.Usage, price.Price, price.MultiplierPPM, h.pricingInput)
		if err != nil {
			return rec, err
		}
		rec.Args = append(rec.Args, "request_procurement_cost_micro", strconv.FormatInt(int64(cost), 10), "request_wallet_before_micro", strconv.FormatInt(int64(value), 10))
	}
	return rec, nil
}
