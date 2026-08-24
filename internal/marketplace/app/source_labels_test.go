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
