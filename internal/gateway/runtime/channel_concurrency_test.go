package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/go-redis/redis/v8"
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
	reserveSharedChannelConcurrency = func(_, _, _, _ int) (func(), bool, ChannelConcurrencyAdmission) {
		return func() {}, false, ChannelConcurrencyAdmitted
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

func (gate *fakeSharedChannelConcurrency) reserve(
	channelID, userID, totalLimit, userLimit int,
) (func(), bool, ChannelConcurrencyAdmission) {
	if totalLimit > 0 && gate.active[channelID] >= totalLimit {
		return func() {}, true, ChannelConcurrencyCapacityReached
	}
	if gate.users[channelID] == nil {
		gate.users[channelID] = make(map[int]int)
	}
	if userLimit > 0 && gate.users[channelID][userID] >= userLimit {
		return func() {}, true, ChannelConcurrencyCapacityReached
	}
	gate.active[channelID]++
	gate.users[channelID][userID]++
	var once sync.Once
	return func() {
		once.Do(func() {
			gate.active[channelID]--
			gate.users[channelID][userID]--
		})
	}, true, ChannelConcurrencyAdmitted
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

	releaseFirst, admission := TryBeginChannelRequestForUser(211, 7, 0, 1)
	require.Equal(t, ChannelConcurrencyAdmitted, admission)
	_, admission = TryBeginChannelRequestForUser(211, 7, 0, 1)
	require.Equal(t, ChannelConcurrencyCapacityReached, admission)

	releaseOther, admission := TryBeginChannelRequestForUser(211, 8, 0, 1)
	require.Equal(t, ChannelConcurrencyAdmitted, admission)
	require.Equal(t, 2, ActiveChannelRequests(211))
	require.Equal(t, 1, ActiveChannelUserRequests(211, 7))
	require.Equal(t, 1, ActiveChannelUserRequests(211, 8))

	releaseFirst()
	releaseOther()
}

func TestChannelConcurrencyRequiresBothTotalAndUserCapacity(t *testing.T) {
	resetChannelConcurrencyForTest(t)

	releaseFirst, admission := TryBeginChannelRequestForUser(221, 7, 2, 2)
	require.Equal(t, ChannelConcurrencyAdmitted, admission)
	releaseSecond, admission := TryBeginChannelRequestForUser(221, 8, 2, 2)
	require.Equal(t, ChannelConcurrencyAdmitted, admission)
	_, admission = TryBeginChannelRequestForUser(221, 9, 2, 2)
	require.Equal(t, ChannelConcurrencyCapacityReached, admission)

	releaseFirst()
	releaseSecond()
}

func TestChannelConcurrencyZeroLimitsRemainUnboundedPerUser(t *testing.T) {
	resetChannelConcurrencyForTest(t)

	for range 3 {
		_, admission := TryBeginChannelRequestForUser(231, 7, 0, 0)
		require.Equal(t, ChannelConcurrencyAdmitted, admission)
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

	release, admission := TryBeginChannelRequestForUser(501, 7, 1, 0)
	require.Equal(t, ChannelConcurrencyAdmitted, admission)
	clearLocalChannelConcurrencyForTest() // Simulate admission in another Gateway process.

	_, admission = TryBeginChannelRequestForUser(501, 8, 1, 0)
	require.Equal(t, ChannelConcurrencyCapacityReached, admission)
	require.Equal(t, 1, gate.active[501])

	release()
	release()
	clearLocalChannelConcurrencyForTest()
	secondRelease, admission := TryBeginChannelRequestForUser(501, 8, 1, 0)
	require.Equal(t, ChannelConcurrencyAdmitted, admission)
	secondRelease()
	require.Zero(t, gate.active[501])
}

func TestChannelConcurrencyEnforcesSharedPerUserLimitAcrossGatewayInstances(t *testing.T) {
	resetChannelConcurrencyForTest(t)
	gate := newFakeSharedChannelConcurrency()
	reserveSharedChannelConcurrency = gate.reserve

	release, admission := TryBeginChannelRequestForUser(511, 7, 10, 1)
	require.Equal(t, ChannelConcurrencyAdmitted, admission)
	clearLocalChannelConcurrencyForTest()

	_, admission = TryBeginChannelRequestForUser(511, 7, 10, 1)
	require.Equal(t, ChannelConcurrencyCapacityReached, admission)
	otherRelease, admission := TryBeginChannelRequestForUser(511, 8, 10, 1)
	require.Equal(t, ChannelConcurrencyAdmitted, admission)

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
	_, enforced, admission := reserveRedisChannelConcurrency(521, 7, 1, 1)
	require.False(t, enforced)
	require.Equal(t, ChannelConcurrencyAdmitted, admission)

	platformcache.RedisEnabled = true
	_, enforced, admission = reserveRedisChannelConcurrency(521, 7, 1, 1)
	require.True(t, enforced)
	require.Equal(t, ChannelConcurrencyDependencyUnavailable, admission)
}

func TestChannelConcurrencyPropagatesRedisDependencyFailure(t *testing.T) {
	resetChannelConcurrencyForTest(t)
	reserveSharedChannelConcurrency = func(_, _, _, _ int) (func(), bool, ChannelConcurrencyAdmission) {
		return func() {}, true, ChannelConcurrencyDependencyUnavailable
	}

	_, admission := TryBeginChannelRequestForUser(525, 7, 10, 1)
	require.Equal(t, ChannelConcurrencyDependencyUnavailable, admission)
	require.Zero(t, ActiveChannelRequests(525))
}

func TestChannelConcurrencyReserveRetriesWithSameToken(t *testing.T) {
	originalRunner := runChannelConcurrencyReserve
	t.Cleanup(func() { runChannelConcurrencyReserve = originalRunner })
	t.Setenv("CHANNEL_CONCURRENCY_REDIS_RESERVE_ATTEMPTS", "2")

	var tokens []string
	committedTokens := make(map[string]struct{})
	runChannelConcurrencyReserve = func(
		_ context.Context,
		_ []string,
		token string,
		_, _ int,
	) (int, error) {
		tokens = append(tokens, token)
		committedTokens[token] = struct{}{}
		if len(tokens) == 1 {
			return 0, errors.New("simulated response timeout after commit")
		}
		return 1, nil
	}

	admitted, err := reserveChannelConcurrencyWithRetry(
		channelConcurrencyLeaseKeys(527, 7),
		"stable-token",
		1,
		1,
	)
	require.NoError(t, err)
	require.Equal(t, 1, admitted)
	require.Equal(t, []string{"stable-token", "stable-token"}, tokens)
	require.Len(t, committedTokens, 1)
}

func TestRedisChannelConcurrencyTimeoutIsDependencyUnavailable(t *testing.T) {
	originalEnabled := platformcache.RedisEnabled
	originalClient := platformcache.RDB
	originalRunner := runChannelConcurrencyReserve
	t.Cleanup(func() {
		platformcache.RedisEnabled = originalEnabled
		platformcache.RDB = originalClient
		runChannelConcurrencyReserve = originalRunner
	})
	t.Setenv("CHANNEL_CONCURRENCY_REDIS_RESERVE_ATTEMPTS", "1")

	platformcache.RedisEnabled = true
	platformcache.RDB = &redis.Client{}
	runChannelConcurrencyReserve = func(
		context.Context,
		[]string,
		string,
		int,
		int,
	) (int, error) {
		return 0, context.DeadlineExceeded
	}

	_, enforced, admission := reserveRedisChannelConcurrency(529, 7, 10, 1)
	require.True(t, enforced)
	require.Equal(t, ChannelConcurrencyDependencyUnavailable, admission)
}

func TestChannelConcurrencyLeaseKeysShareRedisClusterSlot(t *testing.T) {
	keys := channelConcurrencyLeaseKeys(531, 7)
	require.Equal(t, []string{
		"gateway:channel-concurrency:v2:{531}:all",
		"gateway:channel-concurrency:v2:{531}:user:7",
	}, keys)
}
