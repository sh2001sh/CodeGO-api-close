package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMarketplaceDisplayNameUsesSourceMultiplierAndNumericChannelID(t *testing.T) {
	require.Equal(
		t,
		"123456789012-Codex Plus-0.02x",
		marketplaceDisplayName("Codex Plus", 0.02, "123456789012"),
	)
}

func TestCanonicalSourceLabelSupportsCodexMixedPool(t *testing.T) {
	label, ok := canonicalSourceLabel("codex-mixed")
	require.True(t, ok)
	require.Equal(t, "Codex 混合号池", label)
	require.Equal(t, "Codex-Mixed-abc123", marketplaceInternalGroupName(label, "abc123"))
}

func TestMarketplaceSupportsGrokAndGeminiSources(t *testing.T) {
	for _, source := range []string{"Grok", "Gemini"} {
		label, ok := canonicalSourceLabel(source)
		require.True(t, ok)
		require.Equal(t, source, label)
		require.NoError(t, validateSourceLabel("openai_compatible", source))
		require.Equal(t, source+"-abc123", marketplaceInternalGroupName(source, "abc123"))
		require.Equal(t, "163-"+source+"-0.1x", marketplaceDisplayName(source, 0.1, "163"))
	}
}
