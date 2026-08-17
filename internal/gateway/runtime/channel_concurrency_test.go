package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func resetChannelConcurrencyForTest(t *testing.T) {
	t.Helper()
	originalLoader := loadSharedChannelConcurrency
	loadSharedChannelConcurrency = func(_ []int, _ map[int]int) (map[int]int, bool) {
		return nil, false
	}
	channelConcurrency.Lock()
	channelConcurrency.active = make(map[int]int)
	channelConcurrency.users = make(map[int]map[int]int)
	channelConcurrency.Unlock()
	t.Cleanup(func() {
		loadSharedChannelConcurrency = originalLoader
		channelConcurrency.Lock()
		channelConcurrency.active = make(map[int]int)
		channelConcurrency.users = make(map[int]map[int]int)
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

func TestChannelConcurrencyEnforcesPerUserLimitIndependently(t *testing.T) {
	resetChannelConcurrencyForTest(t)

	releaseFirst, admitted := TryBeginChannelRequestForUser(211, 7, 0, 1)
	require.True(t, admitted)
	_, admitted = TryBeginChannelRequestForUser(211, 7, 0, 1)
	require.False(t, admitted)

	releaseOther, admitted := TryBeginChannelRequestForUser(211, 8, 0, 1)
	require.True(t, admitted)
	require.Equal(t, 2, ActiveChannelRequests(211))
	require.Equal(t, 1, ActiveChannelUserRequests(211, 7))
	require.Equal(t, 1, ActiveChannelUserRequests(211, 8))

	releaseFirst()
	releaseOther()
}

func TestChannelConcurrencyRequiresBothTotalAndUserCapacity(t *testing.T) {
	resetChannelConcurrencyForTest(t)

	releaseFirst, admitted := TryBeginChannelRequestForUser(221, 7, 2, 2)
	require.True(t, admitted)
	releaseSecond, admitted := TryBeginChannelRequestForUser(221, 8, 2, 2)
	require.True(t, admitted)
	_, admitted = TryBeginChannelRequestForUser(221, 9, 2, 2)
	require.False(t, admitted)

	releaseFirst()
	releaseSecond()
}

func TestChannelConcurrencyZeroLimitsRemainUnboundedPerUser(t *testing.T) {
	resetChannelConcurrencyForTest(t)

	for range 3 {
		_, admitted := TryBeginChannelRequestForUser(231, 7, 0, 0)
		require.True(t, admitted)
	}
	require.Equal(t, 3, ActiveChannelUserRequests(231, 7))
}

func TestChannelConcurrencyUsesSharedCrossProcessSnapshot(t *testing.T) {
	resetChannelConcurrencyForTest(t)
	release, admitted := TryBeginChannelRequest(301, 10)
	require.True(t, admitted)
	defer release()

	loadSharedChannelConcurrency = func(channelIDs []int, local map[int]int) (map[int]int, bool) {
		require.ElementsMatch(t, []int{301, 302}, channelIDs)
		require.Equal(t, 1, local[301])
		return map[int]int{301: 4, 302: 2}, true
	}

	require.Equal(t, map[int]int{301: 4, 302: 2}, ActiveChannelRequestsForChannels([]int{301, 302}))
}

func TestChannelConcurrencyFallsBackToLocalSnapshot(t *testing.T) {
	resetChannelConcurrencyForTest(t)
	release, admitted := TryBeginChannelRequest(401, 10)
	require.True(t, admitted)
	defer release()

	loadSharedChannelConcurrency = func(_ []int, _ map[int]int) (map[int]int, bool) {
		return nil, false
	}

	require.Equal(t, map[int]int{401: 1, 402: 0}, ActiveChannelRequestsForChannels([]int{401, 402}))
}
