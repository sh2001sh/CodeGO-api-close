package billing

import (
	"maps"

	"github.com/sh2001sh/new-api/v3/internal/billing/pricing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

// The private price copy is already persisted by durable reservations. Never
// annotate the shared catalog or re-read client bytes when settling a hold.
func freezeFastModePrice(req *gateway.Request, price catalog.Price) catalog.Price {
	fast := gateway.FastServiceTier(req) != ""
	if _, exists := price.Rules[pricing.FastModeRule]; !fast && !exists {
		return price
	}
	price.Rules = maps.Clone(price.Rules)
	if price.Rules == nil {
		price.Rules = make(map[string]any)
	}
	price.Rules[pricing.FastModeRule] = fast
	return price
}

func pricingServiceTierMultiplier(h *hold, out gateway.Outcome) int64 {
	price := h.price
	if out.Target != nil && len(h.targetPrices) > 0 {
		price = h.targetPrices[targetPriceKey(*out.Target)].Price
	}
	return pricing.ServiceTierMultiplier(price, out.Usage)
}
