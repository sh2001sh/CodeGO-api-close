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
