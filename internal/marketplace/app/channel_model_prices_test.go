package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeChannelModelPricesScopesAndCanonicalizesModels(t *testing.T) {
	prices, err := normalizeChannelModelPrices(map[string]ChannelModelPrice{
		"GPT-5": {InputPricePerMillion: 2, OutputPricePerMillion: 8},
	}, []string{"gpt-5", "claude-sonnet"})
	require.NoError(t, err)
	require.Equal(t, float64(2), prices["gpt-5"].InputPricePerMillion)
	require.Equal(t, ChannelBillingModeToken, prices["gpt-5"].BillingMode)

	_, err = normalizeChannelModelPrices(map[string]ChannelModelPrice{
		"outside-model": {InputPricePerMillion: 1, OutputPricePerMillion: 2},
	}, []string{"gpt-5"})
	require.ErrorContains(t, err, "不在当前渠道")

	_, err = normalizeChannelModelPrices(map[string]ChannelModelPrice{
		"gpt-5": {InputPricePerMillion: 0, OutputPricePerMillion: 2},
	}, []string{"gpt-5"})
	require.ErrorContains(t, err, "大于 0")
}

func TestNormalizeChannelModelPricesSupportsPerCallAndCachePrices(t *testing.T) {
	cacheRead := 0.0
	cacheWrite := 1.25
	prices, err := normalizeChannelModelPrices(map[string]ChannelModelPrice{
		"token-model": {
			BillingMode:               ChannelBillingModeToken,
			InputPricePerMillion:      2,
			OutputPricePerMillion:     8,
			CacheReadPricePerMillion:  &cacheRead,
			CacheWritePricePerMillion: &cacheWrite,
		},
		"per-call-model": {
			BillingMode:          ChannelBillingModePerCall,
			PricePerCall:         0.03,
			InputPricePerMillion: 999,
		},
	}, []string{"token-model", "per-call-model"})

	require.NoError(t, err)
	require.NotNil(t, prices["token-model"].CacheReadPricePerMillion)
	require.Zero(t, *prices["token-model"].CacheReadPricePerMillion)
	require.Equal(t, 1.25, *prices["token-model"].CacheWritePricePerMillion)
	require.Equal(t, 0.03, prices["per-call-model"].PricePerCall)
	require.Zero(t, prices["per-call-model"].InputPricePerMillion)
}

func TestNormalizeChannelModelPricesRejectsInvalidModeSpecificPrices(t *testing.T) {
	negative := -1.0
	_, err := normalizeChannelModelPrices(map[string]ChannelModelPrice{
		"model": {BillingMode: ChannelBillingModePerCall},
	}, []string{"model"})
	require.ErrorContains(t, err, "每次请求价格")

	_, err = normalizeChannelModelPrices(map[string]ChannelModelPrice{
		"model": {
			InputPricePerMillion:     1,
			OutputPricePerMillion:    2,
			CacheReadPricePerMillion: &negative,
		},
	}, []string{"model"})
	require.ErrorContains(t, err, "缓存读取价格")
}

func TestRetainChannelModelPricesDropsModelsMissingFromReplacement(t *testing.T) {
	prices := retainChannelModelPrices(map[string]ChannelModelPrice{
		"OLD-MODEL": {InputPricePerMillion: 1, OutputPricePerMillion: 2},
		"gpt-5":     {InputPricePerMillion: 3, OutputPricePerMillion: 9},
	}, []string{"GPT-5", "gpt-5-mini"})

	require.Equal(t, map[string]ChannelModelPrice{
		"GPT-5": {InputPricePerMillion: 3, OutputPricePerMillion: 9},
	}, prices)
}
