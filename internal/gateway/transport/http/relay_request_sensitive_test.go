package http

import (
	"testing"

	requestsettings "github.com/sh2001sh/new-api/internal/platform/requestsettings"
	"github.com/stretchr/testify/require"
)

func TestShouldBlockSensitiveWordsHonorsStopOnSensitiveEnabled(t *testing.T) {
	original := requestsettings.StopOnSensitiveEnabled
	t.Cleanup(func() { requestsettings.StopOnSensitiveEnabled = original })

	requestsettings.StopOnSensitiveEnabled = false
	require.False(t, shouldBlockSensitiveWords())

	requestsettings.StopOnSensitiveEnabled = true
	require.True(t, shouldBlockSensitiveWords())
}
