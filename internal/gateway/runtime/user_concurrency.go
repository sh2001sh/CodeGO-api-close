package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"

	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
)

// TryBeginUserRequest enforces an operator account restriction independently of
// the token's group, route and bypass flag. The existing atomic lease scripts
// are reused with a separate namespace; no channel telemetry is affected.
func TryBeginUserRequest(ctx context.Context, userID int) (context.Context, func(), ChannelConcurrencyAdmission) {
	limit := platformconfig.UserMaxConcurrentRequests[userID]
	if limit <= 0 {
		return ctx, func() {}, ChannelConcurrencyAdmitted
	}
	if !platformcache.RedisReady() {
		return ctx, func() {}, ChannelConcurrencyDependencyUnavailable
	}
	key := fmt.Sprintf("gateway:user-concurrency:v1:{%d}:all", userID)
	keys := []string{key, key}
	token := channelConcurrencyLeaseToken()
	admitted, err := reserveChannelConcurrencyWithRetry(keys, token, limit, 0)
	if err != nil {
		reportChannelConcurrencyGateError(fmt.Errorf("reserve user %d lease: %w", userID, err))
		return ctx, func() {}, ChannelConcurrencyDependencyUnavailable
	}
	if admitted != 1 {
		return ctx, func() {}, ChannelConcurrencyCapacityReached
	}
	requestCtx, cancelRequest := context.WithCancel(ctx)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(channelConcurrencyRenewInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
			}
			opCtx, cancel := context.WithTimeout(context.Background(), channelConcurrencyGateOperationTimeout())
			renewed, err := channelConcurrencyRenewScript.Run(opCtx, platformcache.RDB, keys,
				token, channelConcurrencyLeaseTTL.Milliseconds(), limit, 0).Int()
			cancel()
			if err != nil || renewed != 1 {
				reportChannelConcurrencyGateError(fmt.Errorf("user %d concurrency lease lost: renewed=%d error=%v", userID, renewed, err))
				cancelRequest()
				return
			}
		}
	}()
	var once sync.Once
	return requestCtx, func() {
		once.Do(func() {
			close(stop)
			<-done
			cancelRequest()
			releaseRedisChannelConcurrencyLease(keys, token, userID, 0)
		})
	}, ChannelConcurrencyAdmitted
}
