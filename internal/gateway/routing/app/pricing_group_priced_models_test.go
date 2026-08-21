package app

import (
	"strings"
	"testing"

	gatewaydomain "github.com/sh2001sh/new-api/internal/gateway/domain"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	"github.com/stretchr/testify/require"
)

func TestLoadPricedModelNamesIncludesTieredModelsWithoutProjection(t *testing.T) {
	models := loadPricedModelNames(nil)
	foundLuna := false
	for _, model := range models {
		if strings.EqualFold(model, "gpt-5.6-luna") {
			foundLuna = true
			break
		}
	}
	require.True(t, foundLuna)
}

func TestLoadPricedModelDetailsIncludesConfigOnlyModel(t *testing.T) {
	gatewaystore.InitRatioSettings()
	details := loadPricedModelDetails(nil)
	for _, detail := range details {
		if !strings.EqualFold(detail.ModelName, "claude-fable-5") {
			continue
		}
		require.Equal(t, 0, detail.QuotaType)
		require.Equal(t, 0.5, detail.ModelRatio)
		require.Greater(t, detail.CompletionRatio, float64(0))
		require.Empty(t, detail.EnableGroup)
		return
	}
	t.Fatal("claude-fable-5 billing details not found")
}

func TestLoadPricedModelDetailsPreservesMetadataWithoutInternalGroups(t *testing.T) {
	cacheRatio := 0.1
	details := loadPricedModelDetails([]gatewaydomain.Pricing{{
		ModelName: "market-only-model", ModelRatio: 1.25, CompletionRatio: 4,
		CacheRatio: &cacheRatio, VendorID: 2, EnableGroup: []string{"private-market-group"},
	}})

	var found gatewaydomain.Pricing
	for _, detail := range details {
		if detail.ModelName == "market-only-model" {
			found = detail
			break
		}
	}
	require.Equal(t, 1.25, found.ModelRatio)
	require.Equal(t, 4.0, found.CompletionRatio)
	require.Equal(t, 2, found.VendorID)
	require.NotNil(t, found.CacheRatio)
	require.Equal(t, 0.1, *found.CacheRatio)
	require.Empty(t, found.EnableGroup)
}
