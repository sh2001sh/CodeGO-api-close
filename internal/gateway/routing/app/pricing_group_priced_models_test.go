package app

import (
	"strings"
	"testing"

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
