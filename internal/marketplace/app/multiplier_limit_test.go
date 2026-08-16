package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMarketplaceMultiplierLimit(t *testing.T) {
	require.NoError(t, ValidateMultiplierLimitValue(0))
	require.NoError(t, ValidateMultiplierLimitValue(0.01))
	require.Error(t, ValidateMultiplierLimitValue(-0.01))
	require.Error(t, ValidateMultiplierLimitValue(MaxMarketplaceMultiplierLimit+1))

	require.NoError(t, EnforceMultiplierLimit(2, 0))
	require.NoError(t, EnforceMultiplierLimit(0.5, 0.5))
	require.EqualError(
		t,
		EnforceMultiplierLimit(0.51, 0.5),
		"当前第三方渠道倍率 0.51x 超过 API Key 上限 0.5x，请调整分组或倍率上限",
	)
}
