package app

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	"github.com/stretchr/testify/require"
)

func TestDesktopImportCodeConsumptionIsAtomicAndExpires(t *testing.T) {
	oldRedis := platformcache.RedisEnabled
	platformcache.RedisEnabled = false
	t.Cleanup(func() { platformcache.RedisEnabled = oldRedis })
	cache := getDesktopImportCache()
	code := strings.Repeat("a", 32)
	require.NoError(t, cache.SetWithTTL(code, DesktopImportConfigPayload{Tool: "codex", APIKey: "test-key"}, time.Minute))
	start := make(chan struct{})
	var wg sync.WaitGroup
	var successes atomic.Int64
	for range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			payload, err := ResolveDesktopImportConfig(code)
			if err == nil && payload.APIKey == "test-key" {
				successes.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	require.EqualValues(t, 1, successes.Load())
	_, err := ResolveDesktopImportConfig(code)
	require.ErrorContains(t, err, "invalid or expired")
	stored, _, err := cache.Get(code)
	require.NoError(t, err)
	require.Empty(t, stored.APIKey, "consumed cache entries must not retain the credential")
	expired := strings.Repeat("b", 32)
	require.NoError(t, cache.SetWithTTL(expired, DesktopImportConfigPayload{APIKey: "expired-key"}, time.Millisecond))
	time.Sleep(5 * time.Millisecond)
	_, err = ResolveDesktopImportConfig(expired)
	require.ErrorContains(t, err, "invalid or expired")
	_, err = ResolveDesktopImportConfig("wrong")
	require.ErrorContains(t, err, "invalid or expired")
	require.Equal(t, 2*time.Minute, desktopImportCodeTTL)
}
