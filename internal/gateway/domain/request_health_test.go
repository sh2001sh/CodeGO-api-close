package domain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClassifyRequestHealth(t *testing.T) {
	t.Parallel()

	require.Equal(t, RequestHealthUnknown, ClassifyRequestHealth(100, 0))
	require.Equal(t, RequestHealthHealthy, ClassifyRequestHealth(100, 1))
	require.Equal(t, RequestHealthHealthy, ClassifyRequestHealth(90, 10))
	require.Equal(t, RequestHealthUnstable, ClassifyRequestHealth(89.99, 10))
	require.Equal(t, RequestHealthUnstable, ClassifyRequestHealth(85, 10))
	require.Equal(t, RequestHealthFailed, ClassifyRequestHealth(84.99, 10))
}
