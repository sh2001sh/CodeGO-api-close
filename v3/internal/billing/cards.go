package billing

import (
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing/pricing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// Only consumption cards change charged money. Package/monthly/zero-hour
// cards influence routing or funding; their stored limits are legacy facts.
func activeCards(source []catalog.MultiplierCard, now time.Time) []catalog.MultiplierCard {
	var cards []catalog.MultiplierCard
	for _, c := range source {
		if c.IsConsumptionDiscount() && c.ID > 0 && c.MultiplierPPM >= 0 && c.MultiplierPPM < 1000000 && !c.ExpiresAt.IsZero() && now.Before(c.ExpiresAt) {
			cards = append(cards, c)
		}
	}
	slices.SortFunc(cards, func(a, b catalog.MultiplierCard) int {
		if a.MultiplierPPM != b.MultiplierPPM {
			return int(a.MultiplierPPM - b.MultiplierPPM)
		}
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return cards
}

// V2 applies consumption discounts to the already published charged amount,
// including absolute tool prices. Preserve that integer ingress and use exact
// ppm arithmetic; never multiply group and card floats and refix the result.
func consumptionChargeQuantum(before credits.Micro, ppm, quantum int64) credits.Micro {
	n := new(big.Int).Mul(big.NewInt(int64(before)), big.NewInt(ppm))
	d := big.NewInt(1_000_000 * quantum)
	n.Mul(n, big.NewInt(2)).Add(n, d)
	n.Quo(n, d.Mul(d, big.NewInt(2))).Mul(n, big.NewInt(quantum))
	return credits.Micro(n.Int64())
}

func holdQuantum(h *hold, target *gateway.Target) (int64, error) {
	p := h.price
	if target != nil && len(h.targetPrices) > 0 {
		p = h.targetPrices[targetPriceKey(*target)].Price
	}
	return pricing.MoneyQuantum(p)
}

func (s *Settler) settlementPrice(h *hold, out gateway.Outcome) (credits.Micro, error) {
	if !out.Charge {
		return 0, nil
	}
	if h.sourceMode {
		if out.Target == nil {
			return 0, fmt.Errorf("billing: missing source target")
		}
		quote, err := s.quoteSource(h, *out.Target, out.Usage)
		return quote.Wallet, err
	}
	before, err := s.targetCharge(h, out)
	if err != nil {
		return 0, err
	}
	if len(h.cards) > 0 && out.Target != nil && h.cardChannels[out.Target.ChannelID] {
		quantum, err := holdQuantum(h, out.Target)
		if err != nil {
			return 0, err
		}
		return consumptionChargeQuantum(before, h.cards[0].MultiplierPPM, quantum), nil
	}
	return before, nil
}

func (s *Settler) appendCardCall(rec walRecord, h *hold, out gateway.Outcome) (walRecord, error) {
	if h.sourceMode {
		return rec, nil
	}
	if !out.Charge || out.Target == nil || !h.cardChannels[out.Target.ChannelID] || len(h.cards) == 0 {
		return rec, nil
	}
	before, err := s.targetCharge(h, out)
	if err != nil {
		return rec, err
	}
	quantum, err := holdQuantum(h, out.Target)
	if err != nil {
		return rec, err
	}
	after := consumptionChargeQuantum(before, h.cards[0].MultiplierPPM, quantum)
	if before > after {
		rec.Args = append(rec.Args, FieldCardID, strconv.FormatInt(h.cards[0].ID, 10),
			FieldCardBefore, strconv.FormatInt(int64(before), 10), FieldCardAfter, strconv.FormatInt(int64(after), 10))
	}
	return rec, nil
}
