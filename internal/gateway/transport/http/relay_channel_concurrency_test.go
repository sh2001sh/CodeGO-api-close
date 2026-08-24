package http

import (
	"net/http"
	"testing"

	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestChannelConcurrencyCapacityRejectionRemainsRetryable(t *testing.T) {
	reason, apiErr, retryOtherChannel := channelConcurrencyRejection(
		relaycommon.ChannelConcurrencyCapacityReached,
	)

	require.Equal(t, "channel_capacity", reason)
	require.Equal(t, types.ErrorCodeGetChannelFailed, apiErr.GetErrorCode())
	require.Equal(t, http.StatusServiceUnavailable, apiErr.StatusCode)
	require.True(t, retryOtherChannel)
	require.False(t, types.IsSkipRetryError(apiErr))
}

func TestChannelConcurrencyDependencyRejectionIsServiceBusy(t *testing.T) {
	reason, apiErr, retryOtherChannel := channelConcurrencyRejection(
		relaycommon.ChannelConcurrencyDependencyUnavailable,
	)

	require.Equal(t, "channel_concurrency_dependency", reason)
	require.Equal(t, types.ErrorCodeServiceBusy, apiErr.GetErrorCode())
	require.Equal(t, http.StatusServiceUnavailable, apiErr.StatusCode)
	require.False(t, retryOtherChannel)
	require.True(t, types.IsSkipRetryError(apiErr))
}
