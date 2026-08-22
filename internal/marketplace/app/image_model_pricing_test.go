package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImageModelRequiresPerCallMarketplacePrice(t *testing.T) {
	models := []string{"gpt-image-2"}
	require.Error(t, validateImageModelPrices(nil, models))
	require.Error(t, validateImageModelPrices(map[string]ChannelModelPrice{
		"gpt-image-2": {InputPricePerMillion: 1, OutputPricePerMillion: 1},
	}, models))
	require.NoError(t, validateImageModelPrices(map[string]ChannelModelPrice{
		"gpt-image-2": {BillingMode: ChannelBillingModePerCall, PricePerCall: 0.03},
	}, models))
}

func TestVerifiableMarketplaceModelsSkipsImageModels(t *testing.T) {
	require.Empty(t, verifiableMarketplaceModels([]string{"gpt-image-2"}))
	require.Equal(t, []string{"gpt-5.6-sol"}, verifiableMarketplaceModels([]string{
		"gpt-image-2", "gpt-5.6-sol",
	}))
}
