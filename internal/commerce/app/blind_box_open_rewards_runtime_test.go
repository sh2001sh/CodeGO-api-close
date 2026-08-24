package app

import (
	"math"
	"testing"

	blindboxsettings "github.com/sh2001sh/new-api/internal/commerce/blindboxsettings"
	"github.com/stretchr/testify/assert"
)

func TestPickBlindBoxTierNormalizesIncompleteProbability(t *testing.T) {
	tiers := []blindboxsettings.TierSetting{
		{Name: "first", Probability: 0.1},
		{Name: "second", Probability: 0.1},
		{Name: "third", Probability: 0.1},
	}

	tier := pickBlindBoxTierForRoll(tiers, 0.4)
	assert.Equal(t, "second", tier.Name)
}

func TestDefaultBalanceBlindBoxProbabilitiesSumToOne(t *testing.T) {
	setting := blindboxsettings.Get()
	total := 0.0
	for _, tier := range setting.BalanceBlindBoxTiers {
		total += tier.Probability
	}
	assert.LessOrEqual(t, math.Abs(total-1), 0.00000001)
}
