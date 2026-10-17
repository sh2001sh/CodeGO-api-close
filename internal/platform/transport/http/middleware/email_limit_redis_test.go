package middleware

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	"github.com/stretchr/testify/require"
)

func TestEmailLimitIsolatedRedisAtomicityAndFailure(t *testing.T) {
	addr := os.Getenv("IDENTITY_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("requires isolated Redis on a loopback non-production port")
	}
	host, port, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	require.True(t, net.ParseIP(host).IsLoopback() && port != "6379")
	a, b := redis.NewClient(&redis.Options{Addr: addr}), redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { a.Close(); b.Close() })
	ctx := context.Background()
	key := fmt.Sprintf("emailVerification:isolated-identity-test:%d", time.Now().UnixNano())
	t.Cleanup(func() { require.NoError(t, a.Del(ctx, key).Err()) })
	const workers = 32
	results := make(chan int64, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			client := a
			if i%2 != 0 {
				client = b
			}
			n, err := emailVerificationLimitScript.Run(ctx, client, []string{key}, EmailVerificationDuration).Int64()
			if err != nil {
				results <- -1
				return
			}
			results <- n
		}(i)
	}
	wg.Wait()
	close(results)
	accepted := 0
	for n := range results {
		require.Greater(t, n, int64(0))
		if n <= EmailVerificationMaxRequests {
			accepted++
		}
	}
	require.Equal(t, EmailVerificationMaxRequests, accepted)
	require.Greater(t, a.TTL(ctx, key).Val(), time.Duration(0))
	require.NoError(t, a.Persist(ctx, key).Err())
	_, err = emailVerificationLimitScript.Run(ctx, b, []string{key}, EmailVerificationDuration).Int64()
	require.NoError(t, err)
	require.Greater(t, a.TTL(ctx, key).Val(), time.Duration(0), "orphaned counter TTL must recover atomically")
	oldRedis, oldRDB := platformcache.RedisEnabled, platformcache.RDB
	broken := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", MaxRetries: -1, DialTimeout: 20 * time.Millisecond})
	platformcache.RedisEnabled, platformcache.RDB = true, broken
	t.Cleanup(func() { platformcache.RedisEnabled, platformcache.RDB = oldRedis, oldRDB; broken.Close() })
	engine := gin.New()
	engine.GET("/", EmailVerificationRateLimit(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}
