package runtime

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestChannelHealthIsIsolatedByRequestType(t *testing.T) {
	require.NoError(t, resetChannelHealthForTest())
	t.Cleanup(func() { require.NoError(t, resetChannelHealthForTest()) })

	for range channelHealthRetryableFailureThreshold {
		RecordChannelRetryableFailure(42, "gpt-health-v2", RequestTypeChatShortStream)
	}

	require.True(t, IsChannelCooling(42, "gpt-health-v2", RequestTypeChatShortStream))
	require.False(t, IsChannelCooling(42, "gpt-health-v2", RequestTypeChatLongStream))
	_, found := GetChannelHealth(42, "gpt-health-v2", RequestTypeChatLongStream)
	require.False(t, found)
}

func TestFaultDomainHealthIsIsolatedByRequestType(t *testing.T) {
	require.NoError(t, resetChannelHealthForTest())
	t.Cleanup(func() { require.NoError(t, resetChannelHealthForTest()) })

	domain := "provider:health-v2"
	for requestID := range faultDomainFailureThreshold {
		RecordFaultDomainRetryableFailure(
			domain,
			"gpt-health-v2",
			strconv.Itoa(requestID+1),
			15*time.Second,
			RequestTypeImageNonStream,
		)
	}

	require.True(t, IsFaultDomainCooling(domain, "gpt-health-v2", RequestTypeImageNonStream))
	require.False(t, IsFaultDomainCooling(domain, "gpt-health-v2", RequestTypeChatShortStream))
}
