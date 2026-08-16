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

	_, err = normalizeChannelModelPrices(map[string]ChannelModelPrice{
		"outside-model": {InputPricePerMillion: 1, OutputPricePerMillion: 2},
	}, []string{"gpt-5"})
	require.ErrorContains(t, err, "不在当前渠道")

	_, err = normalizeChannelModelPrices(map[string]ChannelModelPrice{
		"gpt-5": {InputPricePerMillion: 0, OutputPricePerMillion: 2},
	}, []string{"gpt-5"})
	require.ErrorContains(t, err, "大于 0")
}
