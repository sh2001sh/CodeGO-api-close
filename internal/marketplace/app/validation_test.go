package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateMarketplaceURLRejectsPrivateDestinations(t *testing.T) {
	t.Parallel()

	for _, target := range []string{
		"http://api.example.com",
		"https://127.0.0.1",
		"https://10.0.0.1",
		"https://169.254.169.254/latest/meta-data",
		"https://[::1]",
	} {
		t.Run(target, func(t *testing.T) {
			require.Error(t, ValidateMarketplaceURL(target))
		})
	}
}

func TestValidateMultiplierAllowsAnyFinitePositiveValue(t *testing.T) {
	require.NoError(t, validateMultiplier(0.0001))
	require.NoError(t, validateMultiplier(9999))
	require.Error(t, validateMultiplier(0))
	require.Error(t, validateMultiplier(-1))
}

func TestValidateSourceLabel(t *testing.T) {
	require.NoError(t, validateSourceLabel("anthropic", "CC-Max"))
	require.NoError(t, validateSourceLabel("codex", "Plus"))
	require.NoError(t, validateSourceLabel("codex", "Codex Pro"))
	require.Error(t, validateSourceLabel("anthropic", ""))
	require.Error(t, validateSourceLabel("codex", "https://example.com"))
}
