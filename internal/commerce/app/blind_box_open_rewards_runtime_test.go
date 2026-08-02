package app

import (
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
