package runtime

import (
	"sync"
	"testing"

	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	"github.com/stretchr/testify/require"
)

func resetChannelConcurrencyForTest(t *testing.T) {
	t.Helper()
	originalLoader := loadSharedChannelConcurrency
	originalReserve := reserveSharedChannelConcurrency
	loadSharedChannelConcurrency = func(_ []int, _ map[int]int) (map[int]int, bool) {
		return nil, false
	}
	reserveSharedChannelConcurrency = func(_, _, _, _ int) (func(), bool, bool) {
		return func() {}, false, true
	}
	channelConcurrency.Lock()
	channelConcurrency.active = make(map[int]int)
	channelConcurrency.users = make(map[int]map[int]int)
	channelConcurrency.Unlock()
	t.Cleanup(func() {
		loadSharedChannelConcurrency = originalLoader
		reserveSharedChannelConcurrency = originalReserve
		channelConcurrency.Lock()
		channelConcurrency.active = make(map[int]int)
		channelConcurrency.users = make(map[int]map[int]int)
		channelConcurrency.Unlock()
	})
}

type fakeSharedChannelConcurrency struct {
	active map[int]int
	users  map[int]map[int]int
}

func newFakeSharedChannelConcurrency() *fakeSharedChannelConcurrency {
	return &fakeSharedChannelConcurrency{
		active: make(map[int]int),
		users:  make(map[int]map[int]int),
	}
}

func (gate *fakeSharedChannelConcurrency) reserve(channelID, userID, totalLimit, userLimit int) (func(), bool, bool) {
	if totalLimit > 0 && gate.active[channelID] >= totalLimit {
		return func() {}, true, false
	}
	if gate.users[channelID] == nil {
		gate.users[channelID] = make(map[int]int)
	}
	if userLimit > 0 && gate.users[channelID][userID] >= userLimit {
		return func() {}, true, false
	}
	gate.active[channelID]++
	gate.users[channelID][userID]++
	var once sync.Once
	return func() {
		once.Do(func() {
			gate.active[channelID]--
			gate.users[channelID][userID]--
		})
	}, true, true
}

func clearLocalChannelConcurrencyForTest() {
	channelConcurrency.Lock()
	channelConcurrency.active = make(map[int]int)
	channelConcurrency.users = make(map[int]map[int]int)
	channelConcurrency.Unlock()
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

func TestChannelConcurrencyEnforcesSharedLimitAcrossGatewayInstances(t *testing.T) {
	resetChannelConcurrencyForTest(t)
	gate := newFakeSharedChannelConcurrency()
	reserveSharedChannelConcurrency = gate.reserve

	release, admitted := TryBeginChannelRequestForUser(501, 7, 1, 0)
	require.True(t, admitted)
	clearLocalChannelConcurrencyForTest() // Simulate admission in another Gateway process.

	_, admitted = TryBeginChannelRequestForUser(501, 8, 1, 0)
	require.False(t, admitted)
	require.Equal(t, 1, gate.active[501])

	release()
	release()
	clearLocalChannelConcurrencyForTest()
	secondRelease, admitted := TryBeginChannelRequestForUser(501, 8, 1, 0)
	require.True(t, admitted)
	secondRelease()
	require.Zero(t, gate.active[501])
}

func TestChannelConcurrencyEnforcesSharedPerUserLimitAcrossGatewayInstances(t *testing.T) {
	resetChannelConcurrencyForTest(t)
	gate := newFakeSharedChannelConcurrency()
	reserveSharedChannelConcurrency = gate.reserve

	release, admitted := TryBeginChannelRequestForUser(511, 7, 10, 1)
	require.True(t, admitted)
	clearLocalChannelConcurrencyForTest()

	_, admitted = TryBeginChannelRequestForUser(511, 7, 10, 1)
	require.False(t, admitted)
	otherRelease, admitted := TryBeginChannelRequestForUser(511, 8, 10, 1)
	require.True(t, admitted)

	release()
	otherRelease()
	require.Zero(t, gate.active[511])
}

func TestRedisChannelConcurrencyGateFallsBackOnlyWhenRedisIsDisabled(t *testing.T) {
	originalEnabled := platformcache.RedisEnabled
	originalClient := platformcache.RDB
	t.Cleanup(func() {
		platformcache.RedisEnabled = originalEnabled
		platformcache.RDB = originalClient
	})

	platformcache.RedisEnabled = false
	platformcache.RDB = nil
	_, enforced, admitted := reserveRedisChannelConcurrency(521, 7, 1, 1)
	require.False(t, enforced)
	require.True(t, admitted)

	platformcache.RedisEnabled = true
	_, enforced, admitted = reserveRedisChannelConcurrency(521, 7, 1, 1)
	require.True(t, enforced)
	require.False(t, admitted)
}

func TestChannelConcurrencyLeaseKeysShareRedisClusterSlot(t *testing.T) {
	keys := channelConcurrencyLeaseKeys(531, 7)
	require.Equal(t, []string{
		"gateway:channel-concurrency:v2:{531}:all",
		"gateway:channel-concurrency:v2:{531}:user:7",
	}, keys)
}
