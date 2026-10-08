package billing

import (
	"fmt"
	"strconv"

	"github.com/sh2001sh/new-api/v3/internal/billing/pricing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// targetPrice is serializable so durable tasks retain admission pricing across
// catalog rotations. Price.Rules is immutable after admission, including any
// private Veo request facts added to a copy of the catalog price.
type targetPrice struct {
	SubscriptionPolicies         map[int64]subscriptionPrice `json:"subscription_policies,omitempty"`
	Price                        catalog.Price               `json:"price"`
	MultiplierPPM                int64                       `json:"multiplier_ppm"`
	Group                        string                      `json:"group"`
	Market                       bool                        `json:"market"`
	SubscriptionAllowed          bool                        `json:"subscription_allowed,omitempty"`
	SubscriptionFactorPPM        int64                       `json:"subscription_factor_ppm,omitempty"`
	PackagePPM                   int64                       `json:"package_ppm,omitempty"`
	SubscriptionAccounts         map[int64]bool              `json:"subscription_accounts,omitempty"`
	RoutePoolID                  int64                       `json:"route_pool_id,omitempty"`
	ProcurementCostMultiplierPPM int64                       `json:"procurement_cost_multiplier_ppm,omitempty"`
}

type subscriptionPrice struct {
	PolicyVersion        string `json:"policy_version"`
	OrderID              int64  `json:"order_id,omitempty"`
	RevenueMultiplierPPM *int64 `json:"revenue_multiplier_ppm,omitempty"`
}

func targetPriceKey(target gateway.Target) string {
	return strconv.FormatInt(target.ChannelID, 10) + ":" + strconv.FormatInt(target.CredentialID, 10) + ":" + target.Group
}

func (s *Settler) freezeTargetPrices(req *gateway.Request, snap *catalog.Snapshot) (map[string]targetPrice, catalog.Price, float64, error) {
	if snap == nil {
		return nil, catalog.Price{}, 0, fmt.Errorf("%w: catalog not ready", gateway.ErrBillingUnavailable)
	}
	compiled := false
	for _, target := range req.Targets {
		compiled = compiled || target.Group != ""
	}
	if !compiled {
		return s.freezeUncompiledPrice(req, snap)
	}
	return s.freezeCompiledTargetPrices(req, snap)
}

// freezeUncompiledPrice handles requests that didn't go through catalog
// routing's compiled pricing groups: a single catalog price applies to the
// whole request, and market channels are rejected since they require a
// compiled group.
func (s *Settler) freezeUncompiledPrice(req *gateway.Request, snap *catalog.Snapshot) (map[string]targetPrice, catalog.Price, float64, error) {
	for _, target := range req.Targets {
		if _, market := snap.Market.Channels[target.ChannelID]; market {
			return nil, catalog.Price{}, 0, fmt.Errorf("%w: market target requires a compiled pricing group", gateway.ErrBillingUnavailable)
		}
	}
	price, multiplier, err := s.requestPrice(req, snap)
	if err != nil {
		return nil, price, multiplier, err
	}
	target := gateway.Target{}
	if len(req.Targets) > 0 {
		target = req.Targets[0]
	}
	price, err = freezeVideoPrice(req, price, target)
	return nil, price, multiplier, err
}

// freezeCompiledTargetPrices resolves and freezes a per-target price for
// every compiled routing target, keyed by targetPriceKey. The first target's
// price and multiplier are also returned for callers that only need one
// (non-funding reservation sizing).
func (s *Settler) freezeCompiledTargetPrices(req *gateway.Request, snap *catalog.Snapshot) (map[string]targetPrice, catalog.Price, float64, error) {
	frozen := make(map[string]targetPrice, len(req.Targets))
	var first targetPrice
	for index, target := range req.Targets {
		value, err := s.freezeOneTargetPrice(req, snap, target)
		if err != nil {
			return nil, catalog.Price{}, 0, err
		}
		key := targetPriceKey(target)
		if _, exists := frozen[key]; exists {
			return nil, catalog.Price{}, 0, fmt.Errorf("%w: duplicate pricing target", gateway.ErrBillingUnavailable)
		}
		frozen[key] = value
		if index == 0 {
			first = value
		}
	}
	return frozen, first.Price, float64(first.MultiplierPPM) / 1_000_000, nil
}

// freezeOneTargetPrice resolves, validates, and freezes the price for a
// single compiled routing target.
func (s *Settler) freezeOneTargetPrice(req *gateway.Request, snap *catalog.Snapshot, target gateway.Target) (targetPrice, error) {
	var factor int64
	if target.Group != "" {
		if target.ChannelID <= 0 || target.CredentialID <= 0 || snap.Channels[target.ChannelID] == nil || target.MultiplierPPM < 0 {
			return targetPrice{}, fmt.Errorf("%w: invalid pricing target", gateway.ErrBillingUnavailable)
		}
		factor = target.MultiplierPPM
	} else {
		return targetPrice{}, fmt.Errorf("%w: incomplete compiled pricing target", gateway.ErrBillingUnavailable)
	}
	price, found := snap.Prices[req.Model]
	market, ownerPrice := snap.Market.Channels[target.ChannelID]
	if ownerPrice && len(market.ModelPrices) > 0 {
		price, found = market.ModelPrices[req.Model]
	}
	if !found {
		return targetPrice{}, fmt.Errorf("%w: no price for model %s on channel %d", gateway.ErrBillingUnavailable, req.Model, target.ChannelID)
	}
	price, err := freezeVideoPrice(req, price, target)
	if err != nil {
		return targetPrice{}, fmt.Errorf("%w: video parameters: %v", gateway.ErrBillingUnavailable, err)
	}
	if err := pricing.Validate(price); err != nil {
		return targetPrice{}, fmt.Errorf("%w: target price: %v", gateway.ErrBillingUnavailable, err)
	}
	value := targetPrice{Price: price, MultiplierPPM: factor, Group: target.Group, Market: ownerPrice}
	poolID, procurementPPM, err := freezeProcurement(target)
	if err != nil {
		return targetPrice{}, fmt.Errorf("%w: procurement: %v", gateway.ErrBillingUnavailable, err)
	}
	value.RoutePoolID, value.ProcurementCostMultiplierPPM = poolID, procurementPPM
	return value, nil
}

func (s *Settler) estimateTargetPrices(req *gateway.Request, frozen map[string]targetPrice, price catalog.Price, multiplier float64, input pricing.RequestInput, cards []catalog.MultiplierCard, eligible map[int64]bool) (credits.Micro, error) {
	if len(frozen) == 0 {
		usage, err := pricing.EstimateUsageForPrice(req.Body, price, s.cfg.Estimate)
		if err != nil {
			return 0, err
		}
		amount, err := pricing.PriceForRequest(usage, price, multiplier, input)
		if err != nil {
			return 0, err
		}
		allEligible := len(req.Targets) > 0 && len(cards) > 0
		for _, target := range req.Targets {
			allEligible = allEligible && eligible[target.ChannelID]
		}
		if allEligible {
			quantum, err := pricing.MoneyQuantum(price)
			if err != nil {
				return 0, err
			}
			amount = consumptionChargeQuantum(amount, cards[0].MultiplierPPM, quantum)
		}
		return amount, nil
	}
	var maximum credits.Micro
	for _, target := range req.Targets {
		value := frozen[targetPriceKey(target)]
		usage, err := pricing.EstimateUsageForPrice(req.Body, value.Price, s.cfg.Estimate)
		if err != nil {
			return 0, err
		}
		amount, err := pricing.PriceForRequestPPM(usage, value.Price, value.MultiplierPPM, input)
		if err != nil {
			return 0, err
		}
		if len(cards) > 0 && eligible[target.ChannelID] {
			quantum, err := pricing.MoneyQuantum(value.Price)
			if err != nil {
				return 0, err
			}
			amount = consumptionChargeQuantum(amount, cards[0].MultiplierPPM, quantum)
		}
		maximum = max(maximum, amount)
	}
	return maximum, nil
}

func (s *Settler) targetCharge(h *hold, out gateway.Outcome) (credits.Micro, error) {
	if len(h.targetPrices) == 0 {
		return pricing.PriceForRequest(out.Usage, h.price, h.multiplier, h.pricingInput)
	}
	if out.Target == nil {
		return 0, fmt.Errorf("billing: charged settlement requires an admitted target")
	}
	value, ok := h.targetPrices[targetPriceKey(*out.Target)]
	if !ok {
		return 0, fmt.Errorf("billing: settlement target was not admitted")
	}
	return pricing.PriceForRequestPPM(out.Usage, value.Price, value.MultiplierPPM, h.pricingInput)
}
