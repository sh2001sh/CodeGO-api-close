package billing

import (
	"context"
	"fmt"
	"math/big"

	"github.com/sh2001sh/new-api/v3/internal/billing/pricing"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type sourceQuote struct {
	Wallet, Subscription             credits.Micro
	WalletBefore, SubscriptionBefore credits.Micro
	CardID                           int64
	GrossDivisor                     int64
	Quantum                          int64
}

func sourceScale(amount credits.Micro, numerator, denominator int64) (credits.Micro, error) {
	return sourceScalePackage(amount, numerator, denominator, 1_000_000)
}

func sourceScalePackage(amount credits.Micro, numerator, denominator, packagePPM int64) (credits.Micro, error) {
	return sourceScaleQuantum(amount, numerator, denominator, packagePPM, 1)
}

func sourceScaleQuantum(amount credits.Micro, numerator, denominator, packagePPM, quantum int64) (credits.Micro, error) {
	if amount < 0 || numerator < 0 || denominator <= 0 || packagePPM <= 0 || (quantum != 1 && quantum != 2) {
		return 0, fmt.Errorf("billing: invalid source multiplier")
	}
	n := new(big.Int).Mul(big.NewInt(int64(amount)), big.NewInt(numerator))
	n.Mul(n, big.NewInt(packagePPM))
	d := new(big.Int).Mul(big.NewInt(denominator), big.NewInt(1_000_000))
	d.Mul(d, big.NewInt(quantum))
	n.Mul(n, big.NewInt(2)).Add(n, d)
	n.Quo(n, d.Mul(d, big.NewInt(2)))
	n.Mul(n, big.NewInt(quantum))
	if !n.IsInt64() {
		return 0, credits.ErrOverflow
	}
	// V2's positive minimum is one OLD quota unit (two migrated micro).
	// Native prices retain their one-micro minimum.
	if amount > 0 && numerator > 0 && n.Sign() == 0 {
		return credits.Micro(quantum), nil
	}
	return credits.Micro(n.Int64()), nil
}

func (s *Settler) quoteSource(h *hold, target gateway.Target, usage gateway.Usage) (sourceQuote, error) {
	p, ok := h.targetPrices[targetPriceKey(target)]
	if len(h.targetPrices) == 0 {
		ppm, err := pricing.MultiplierToPPM(h.multiplier)
		if err != nil {
			return sourceQuote{}, err
		}
		p, ok = targetPrice{Price: h.price, MultiplierPPM: ppm, SubscriptionAllowed: true, SubscriptionFactorPPM: ppm, PackagePPM: 1_000_000}, true
		if ppm == 0 {
			p.SubscriptionFactorPPM = 1_000_000
		}
	}
	if !ok {
		return sourceQuote{}, fmt.Errorf("billing: source target was not admitted")
	}
	before, err := pricing.PriceForRequestPPM(usage, p.Price, p.MultiplierPPM, h.pricingInput)
	if err != nil {
		return sourceQuote{}, err
	}
	quantum, err := pricing.MoneyQuantum(p.Price)
	if err != nil {
		return sourceQuote{}, err
	}
	quote := sourceQuote{Wallet: before, WalletBefore: before, GrossDivisor: 1, Quantum: quantum}
	if p.Market {
		quote.GrossDivisor = 10
	}
	if len(h.cards) > 0 && h.cardChannels[target.ChannelID] {
		quote.Wallet = consumptionChargeQuantum(before, h.cards[0].MultiplierPPM, quantum)
		quote.CardID = h.cards[0].ID
	}
	if !p.SubscriptionAllowed {
		return quote, nil
	}
	denominator := p.MultiplierPPM
	if denominator == 0 {
		denominator = 1_000_000
	}
	subscription, err := sourceScaleQuantum(before, p.SubscriptionFactorPPM, denominator, p.PackagePPM, quantum)
	if err != nil {
		return quote, err
	}
	quote.Subscription, quote.SubscriptionBefore = subscription, subscription
	if p.PackagePPM >= 1_000_000 && quote.CardID > 0 {
		quote.Subscription = consumptionChargeQuantum(subscription, h.cards[0].MultiplierPPM, quantum)
	}
	return quote, nil
}

func (s *Settler) sourceReserveArgs(ctx context.Context, req *gateway.Request, h *hold, budgetIndex int) ([]any, error) {
	targets := req.Targets
	if len(h.targetPrices) == 0 && len(targets) == 0 {
		targets = []gateway.Target{{}}
	}
	protocol := sourceProtocol(h)
	lifetime := s.reservationLifetime(ctx)
	args := []any{protocol, int64(s.cfg.OverdraftAllowance), s.cfg.Now().Add(lifetime).UnixMilli(),
		(lifetime + reservationGrace).Milliseconds(), req.ID, len(targets), budgetIndex}
	for _, part := range h.funding {
		args = append(args, part.account)
	}
	for _, part := range h.funding {
		limit := h.sourceLimits[part.account]
		if limit.Limit < 0 || limit.Used < 0 {
			return nil, fmt.Errorf("billing: invalid model source cap")
		}
		args = append(args, limit.ModelKey, limit.Limit, limit.Used)
	}
	pref, err := normalizeFundingPreference(h.fundingPreference, nil)
	if err != nil {
		return nil, err
	}
	args = append(args, pref)
	for _, target := range targets {
		p := h.targetPrices[targetPriceKey(target)]
		if len(h.targetPrices) == 0 {
			p.Price = h.price
		}
		usage, err := pricing.EstimateUsageForPrice(req.Body, p.Price, s.cfg.Estimate)
		if err != nil {
			return nil, err
		}
		quote, err := s.quoteSource(h, target, usage)
		if err != nil {
			return nil, err
		}
		args = append(args, int64(quote.Wallet), int64(quote.Subscription), quote.Quantum)
		for _, part := range h.funding {
			allowed := 0
			if sourceAllows(h, p, part.account) {
				allowed = 1
			}
			args = append(args, allowed)
			if protocol == "source-v2" {
				amount, _, _, _ := bucketQuote(p, quote, part.account)
				args = append(args, int64(amount))
			}
		}
	}
	return args, nil
}

func (s *Settler) sourceFinalizeCall(req *gateway.Request, out gateway.Outcome, h *hold) (walRecord, error) {
	quote := sourceQuote{GrossDivisor: 1, Quantum: 1}
	var selected targetPrice
	if out.Charge {
		if out.Target == nil && len(h.targetPrices) > 0 {
			return walRecord{}, fmt.Errorf("billing: missing source settlement target")
		}
		target := gateway.Target{}
		if out.Target != nil {
			target = *out.Target
		}
		var err error
		quote, err = s.quoteSource(h, target, out.Usage)
		if err != nil {
			return walRecord{}, err
		}
		selected = h.targetPrices[targetPriceKey(target)]
	}
	budgetIndex := 0
	if h.budgetAccount > 0 {
		budgetIndex = len(h.funding)
	}
	protocol := sourceProtocol(h)
	args := []any{protocol, int64(quote.Wallet), int64(quote.Subscription), int64(s.cfg.OverdraftCap), s.cfg.DoneTTL.Milliseconds(),
		req.ID, len(h.funding), budgetIndex, int64(quote.WalletBefore), int64(quote.SubscriptionBefore), quote.CardID, quote.GrossDivisor, quote.Quantum}
	for _, part := range h.funding {
		allowed := 0
		if out.Charge && sourceAllows(h, selected, part.account) {
			allowed = 1
			limit := h.sourceLimits[part.account]
			if limit.ExpiresMillis > s.cfg.Now().UnixMilli() || len(h.targetPrices) == 0 {
				allowed = 2
			}
		}
		limit := h.sourceLimits[part.account]
		args = append(args, part.account, int64(part.amount), allowed, limit.ModelKey, limit.Limit, limit.Used, limit.SubscriptionID)
		if protocol == "source-v2" {
			amount, before, divisor, policy := bucketQuote(selected, quote, part.account)
			revenue, err := frozenRevenue(policy)
			if err != nil {
				return walRecord{}, err
			}
			if part.account == h.account {
				amount, before, divisor, policy = quote.Wallet, quote.WalletBefore, 1, subscriptionPrice{PolicyVersion: "wallet"}
				revenue = ""
			}
			args = append(args, int64(amount), int64(before), divisor, policy.PolicyVersion, policy.OrderID, revenue)
		}
	}
	pref, err := normalizeFundingPreference(h.fundingPreference, nil)
	if err != nil {
		return walRecord{}, err
	}
	if !out.Charge {
		pref = "release"
	}
	args = append(args, FieldFundingPreference, pref)
	if protocol == "source-v2" {
		args = append(args, "funding_full_wallet_before", int64(quote.WalletBefore))
		if out.Charge && selected.RoutePoolID > 0 && selected.ProcurementCostMultiplierPPM > 0 {
			cost, err := pricing.PriceForRequestPPM(out.Usage, selected.Price, selected.ProcurementCostMultiplierPPM, h.pricingInput)
			if err != nil {
				return walRecord{}, err
			}
			args = append(args, "funding_full_procurement_cost", int64(cost))
		}
	}
	rec := s.finalizeCall(req, out, h, quote.Wallet)
	for _, value := range rec.Args[4:] {
		args = append(args, value)
	}
	text := make([]string, len(args))
	for index, value := range args {
		text[index] = fmt.Sprint(value)
	}
	return walRecord{Keys: fundingKeys(h.funding, h.keys), Args: text}, nil
}
