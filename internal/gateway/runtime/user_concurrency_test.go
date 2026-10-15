package runtime

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/go-redis/redis/v8"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	"github.com/stretchr/testify/require"
)

func setupUserConcurrencyTest(t *testing.T) {
	t.Helper()
	oldLimits, oldClient, oldEnabled := platformconfig.UserMaxConcurrentRequests, platformcache.RDB, platformcache.RedisEnabled
	platformconfig.UserMaxConcurrentRequests = map[int]int{900001: 2}
	platformcache.RedisEnabled, platformcache.RDB = false, nil
	t.Cleanup(func() {
		platformconfig.UserMaxConcurrentRequests, platformcache.RDB, platformcache.RedisEnabled = oldLimits, oldClient, oldEnabled
	})
}

func TestUserConcurrencyRestrictionRequiresRedis(t *testing.T) {
	setupUserConcurrencyTest(t)
	_, _, admission := TryBeginUserRequest(context.Background(), 900001)
	require.Equal(t, ChannelConcurrencyDependencyUnavailable, admission)
	_, release, admission := TryBeginUserRequest(context.Background(), 900002)
	require.Equal(t, ChannelConcurrencyAdmitted, admission)
	release()
}

// Run against a dedicated ephemeral Redis instance, never a production DB.
func TestUserConcurrencyRedisIntegration(t *testing.T) {
	addr := os.Getenv("USER_CONCURRENCY_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set USER_CONCURRENCY_TEST_REDIS_ADDR to an isolated Redis instance")
	}
	setupUserConcurrencyTest(t)
	client := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = client.Close() })
	platformcache.RedisEnabled, platformcache.RDB = true, client
	require.NoError(t, client.Ping(context.Background()).Err())
	var admitted atomic.Int32
	var releases sync.Map
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, release, status := TryBeginUserRequest(context.Background(), 900001)
			if status == ChannelConcurrencyAdmitted {
				admitted.Add(1)
				releases.Store(i, release)
			} else {
				require.Equal(t, ChannelConcurrencyCapacityReached, status)
			}
		}(i)
	}
	wg.Wait()
	require.EqualValues(t, 2, admitted.Load())
	_, otherRelease, status := TryBeginUserRequest(context.Background(), 900002)
	require.Equal(t, ChannelConcurrencyAdmitted, status)
	otherRelease()
	releases.Range(func(_, value any) bool { value.(func())(); value.(func())(); return true })
	_, release, status := TryBeginUserRequest(context.Background(), 900001)
	require.Equal(t, ChannelConcurrencyAdmitted, status, "completion must free capacity")
	release()
	require.Zero(t, client.Exists(context.Background(), "gateway:user-concurrency:v1:{900001}:all").Val())
	_ = client.Close()
	t.Setenv("CHANNEL_CONCURRENCY_REDIS_RESERVE_ATTEMPTS", "1")
	_, _, status = TryBeginUserRequest(context.Background(), 900001)
	require.Equal(t, ChannelConcurrencyDependencyUnavailable, status, "Redis failure must not bypass account restriction")
}
