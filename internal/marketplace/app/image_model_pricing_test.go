package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImageModelRequiresPerCallMarketplacePrice(t *testing.T) {
	models := []string{"grok-imagine-image-2.0"}
	require.Error(t, validateImageModelPrices(nil, models))
	require.Error(t, validateImageModelPrices(map[string]ChannelModelPrice{
		"grok-imagine-image-2.0": {InputPricePerMillion: 1, OutputPricePerMillion: 1},
	}, models))
	require.NoError(t, validateImageModelPrices(map[string]ChannelModelPrice{
		"grok-imagine-image-2.0": {BillingMode: ChannelBillingModePerCall, PricePerCall: 0.03},
	}, models))
}

func TestVerifiableMarketplaceModelsSkipsImageModels(t *testing.T) {
	require.Empty(t, verifiableMarketplaceModels([]string{
		"gpt-image-2", "grok-imagine-image", "grok-2-image-1212",
	}))
	require.Equal(t, []string{"gpt-5.6-sol"}, verifiableMarketplaceModels([]string{
		"gpt-image-2", "grok-imagine-image-quality", "gpt-5.6-sol",
	}))
}
