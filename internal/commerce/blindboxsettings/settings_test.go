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
		0.35, 0.18, 0.10, 0.10, 0.18, 0.04, 0.025, 0.006, 0.0015,
		0.001, 0.002, 0.001, 0.0005, 0.0003, 0.0001, 0.0045, 0.0025,
		0.0035, 0.0021,
	}
	for index := range legacy {
		legacy[index].Probability = oldProbabilities[index]
	}
	currentSetting.BalanceBlindBoxTiers = legacy

	got := Get().BalanceBlindBoxTiers
	require.InDelta(t, 0.12, got[0].Probability, 0.000000001)
	require.Equal(t, "$5000 普通额度", got[9].Name)
}
