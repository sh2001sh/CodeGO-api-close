package app

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	"github.com/stretchr/testify/require"
)

func TestIdentityProofIsolatedRedisAtomicity(t *testing.T) {
	addr := os.Getenv("IDENTITY_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("requires isolated test Redis on a loopback non-production port")
	}
	host, port, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	require.True(t, net.ParseIP(host).IsLoopback() && port != "6379", "isolated Redis loopback endpoint required")
	clientA := redis.NewClient(&redis.Options{Addr: addr})
	clientB := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { require.NoError(t, clientA.Close()); require.NoError(t, clientB.Close()) })
	ctx := context.Background()
	require.NoError(t, clientA.Ping(ctx).Err())
	oldRedis, oldRDB := platformcache.RedisEnabled, platformcache.RDB
	platformcache.RedisEnabled, platformcache.RDB = true, clientA
	t.Cleanup(func() { platformcache.RedisEnabled, platformcache.RDB = oldRedis, oldRDB })
	email := fmt.Sprintf("isolated-identity-%d@example.com", time.Now().UnixNano())
	key := verificationStorageKey(email, EmailVerificationPurpose)
	resetKey := verificationStorageKey(email, PasswordResetPurpose)
	replayHash := fmt.Sprintf("isolated-test-%d", time.Now().UnixNano())
	replayKey := "new-api:telegram-replay:v1:" + replayHash
	t.Cleanup(func() { require.NoError(t, clientA.Del(ctx, key, resetKey, replayKey).Err()) })
	require.NoError(t, RegisterVerificationCodeWithKey(email, "123456", EmailVerificationPurpose))
	require.Equal(t, verificationCodeHash("123456"), clientB.HGet(ctx, key, "code").Val())
	require.Greater(t, clientB.TTL(ctx, key).Val(), time.Duration(0))
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				if VerifyCodeWithKey(email, "123456", EmailVerificationPurpose) {
					accepted.Add(1)
				}
			} else {
				n, err := consumeVerificationCode.Run(ctx, clientB, []string{key}, verificationCodeHash("123456"), verificationMaxAttempts).Int()
				if err == nil && n == 1 {
					accepted.Add(1)
				}
			}
		}(i)
	}
	wg.Wait()
	require.EqualValues(t, 1, accepted.Load())
	require.Zero(t, clientB.Exists(ctx, key).Val())
	require.NoError(t, RegisterVerificationCodeWithKey(email, "new-code", EmailVerificationPurpose))
	require.True(t, VerifyCodeWithKey(email, "new-code", EmailVerificationPurpose), "consumption removed replacement code")
	require.NoError(t, RegisterVerificationCodeWithKey(email, "attempt-code", EmailVerificationPurpose))
	for i := 0; i < verificationMaxAttempts; i++ {
		_, err := consumeVerificationCode.Run(ctx, clientB, []string{key}, verificationCodeHash("bad"), verificationMaxAttempts).Int()
		require.NoError(t, err)
	}
	require.False(t, VerifyCodeWithKey(email, "attempt-code", EmailVerificationPurpose))
	require.NoError(t, RegisterVerificationCodeWithKey(email, "expiring", EmailVerificationPurpose))
	require.NoError(t, clientB.PExpire(ctx, key, time.Millisecond).Err())
	time.Sleep(10 * time.Millisecond)
	require.False(t, VerifyCodeWithKey(email, "expiring", EmailVerificationPurpose))
	require.NoError(t, RegisterVerificationCodeWithKey(email, "purpose-code", PasswordResetPurpose))
	require.False(t, VerifyCodeWithKey(email, "purpose-code", EmailVerificationPurpose))
	require.True(t, VerifyCodeWithKey(email, "purpose-code", PasswordResetPurpose))
	accepted.Store(0)
	expiry := time.Now().Add(time.Minute)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				if claimTelegramAuthorization(replayHash, expiry) {
					accepted.Add(1)
				}
			} else if ok, err := clientB.SetNX(ctx, replayKey, "1", time.Until(expiry)).Result(); err == nil && ok {
				accepted.Add(1)
			}
		}(i)
	}
	wg.Wait()
	require.EqualValues(t, 1, accepted.Load())
	require.False(t, claimTelegramAuthorization(replayHash, expiry))
	broken := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", MaxRetries: -1, DialTimeout: 20 * time.Millisecond})
	t.Cleanup(func() { broken.Close() })
	platformcache.RDB = broken
	require.Error(t, RegisterVerificationCodeWithKey(email, "unavailable", EmailVerificationPurpose))
	require.False(t, VerifyCodeWithKey(email, "unavailable", EmailVerificationPurpose))
	require.False(t, claimTelegramAuthorization("unavailable", expiry))
}
