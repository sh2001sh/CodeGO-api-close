package contract

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPath2RelayModeAlphaSearch(t *testing.T) {
	require.Equal(t, RelayModeAlphaSearch, Path2RelayMode("/v1/alpha/search"))
	require.Equal(t, RelayModeAlphaSearch, Path2RelayMode("/v1/alpha/search/preview"))
}
