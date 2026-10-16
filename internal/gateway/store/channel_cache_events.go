package store

import (
	"context"
	"time"

	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
)

const channelCacheChangesTopic = "new-api:channel-cache:changed"

// NotifyChannelCacheChanged is called after committing marketplace changes.
// Gateways refresh immediately; periodic synchronization still covers dropped
// pub/sub messages and Redis outages.
func NotifyChannelCacheChanged() {
	if !platformcache.RedisReady() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := platformcache.RDB.Publish(ctx, channelCacheChangesTopic, "refresh").Err(); err != nil {
		platformobservability.SysError("notify gateway channel cache: " + err.Error())
	}
}
