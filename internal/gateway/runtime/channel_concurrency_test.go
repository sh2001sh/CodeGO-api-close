package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func resetChannelConcurrencyForTest(t *testing.T) {
	t.Helper()
	channelConcurrency.Lock()
	channelConcurrency.active = make(map[int]int)
	channelConcurrency.Unlock()
	t.Cleanup(func() {
		channelConcurrency.Lock()
		channelConcurrency.active = make(map[int]int)
		channelConcurrency.Unlock()
	})
}

func TestChannelConcurrencyEnforcesIndependentLimits(t *testing.T) {
	resetChannelConcurrencyForTest(t)

	releaseFirst, admitted := TryBeginChannelRequest(101, 1)
	require.True(t, admitted)
	_, admitted = TryBeginChannelRequest(101, 1)
	require.False(t, admitted)

	releaseSecond, admitted := TryBeginChannelRequest(102, 1)
	require.True(t, admitted)
	require.Equal(t, 1, ActiveChannelRequests(101))
	require.Equal(t, 1, ActiveChannelRequests(102))

	releaseFirst()
	releaseFirst()
	releaseSecond()
	_, admitted = TryBeginChannelRequest(101, 1)
	require.True(t, admitted)
}

func TestChannelConcurrencyZeroLimitRemainsUnbounded(t *testing.T) {
	resetChannelConcurrencyForTest(t)

	for range 3 {
		_, admitted := TryBeginChannelRequest(201, 0)
		require.True(t, admitted)
	}
	require.Equal(t, 3, ActiveChannelRequests(201))
}
