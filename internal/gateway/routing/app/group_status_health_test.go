package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShouldPreferLogHealthOnlyForShortWindows(t *testing.T) {
	t.Parallel()

	require.True(t, shouldPreferLogHealth(30*60, 30*60))
	require.False(t, shouldPreferLogHealth(24*60*60, 30*60))
	require.False(t, shouldPreferLogHealth(30*60, 60*60))
}
