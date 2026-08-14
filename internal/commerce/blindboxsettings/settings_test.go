package blindboxsettings

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetMigratesPreviousBalanceBlindBoxPool(t *testing.T) {
	original := currentSetting
	t.Cleanup(func() { currentSetting = original })

	legacy := append([]TierSetting(nil), defaultBalanceBlindBoxTiers...)
	oldProbabilities := []float64{
		0.12, 0.17, 0.10, 0.075, 0.19, 0.025, 0.004, 0.00075, 0.00036,
		0.00004, 0.075, 0.19, 0.025, 0.004, 0.00075, 0.007, 0.004, 0.006,
		0.0031,
	}
	for index := range legacy {
		legacy[index].Probability = oldProbabilities[index]
	}
	currentSetting.BalanceBlindBoxTiers = legacy

	got := Get().BalanceBlindBoxTiers
	require.InDelta(t, 0.06, got[0].Probability, 0.000000001)
	require.Equal(t, "$5000 普通额度", got[9].Name)
}
