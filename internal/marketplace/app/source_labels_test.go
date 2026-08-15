package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMarketplaceDisplayNameUsesSourceMultiplierAndNumericChannelID(t *testing.T) {
	require.Equal(
		t,
		"Codex Plus-0.02x-123456789012",
		marketplaceDisplayName("Codex Plus", 0.02, "123456789012"),
	)
}
