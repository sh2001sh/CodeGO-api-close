package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformratelimit "github.com/sh2001sh/new-api/internal/platform/ratelimit"
)

const (
	desktopInvalidTokenCapacity = int64(8)
	desktopInvalidTokenWindow   = 60 * time.Second
)

type desktopInvalidTokenMemoryEntry struct {
	Failures  []time.Time
	BlockedTo time.Time
}

var desktopInvalidTokenMemory = struct {
	sync.Mutex
	entries map[string]desktopInvalidTokenMemoryEntry
}{entries: make(map[string]desktopInvalidTokenMemoryEntry)}

func desktopInvalidTokenKey(c *gin.Context, token string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return fmt.Sprintf("%s:%s", c.ClientIP(), hex.EncodeToString(digest[:8]))
}

// rejectDesktopInvalidTokenFlood returns true only after a token has already
// failed repeatedly. Valid tokens from the same IP are not affected.
func rejectDesktopInvalidTokenFlood(c *gin.Context, token string) bool {
	key := desktopInvalidTokenKey(c, token)
	if platformcache.RedisEnabled && platformcache.RDB != nil {
		blocked, err := platformcache.RDB.Exists(context.Background(), "desktop:invalid:block:"+key).Result()
		if err != nil {
			platformobservability.SysLog(fmt.Sprintf("desktop invalid-token limiter read failed: %v", err))
			return false
		}
		if blocked > 0 {
			writeDesktopInvalidTokenRetry(c)
			return true
		}
		return false
	}

	now := time.Now()
	desktopInvalidTokenMemory.Lock()
	entry, ok := desktopInvalidTokenMemory.entries[key]
	if ok && now.Before(entry.BlockedTo) {
		desktopInvalidTokenMemory.Unlock()
		writeDesktopInvalidTokenRetry(c)
		return true
	}
	if ok && !entry.BlockedTo.IsZero() && !now.Before(entry.BlockedTo) {
		delete(desktopInvalidTokenMemory.entries, key)
	}
	desktopInvalidTokenMemory.Unlock()
	return false
}

func recordDesktopInvalidTokenFailure(c *gin.Context, token string) {
	key := desktopInvalidTokenKey(c, token)
	if platformcache.RedisEnabled && platformcache.RDB != nil {
		allowed, err := platformratelimit.NewRedisLimiter(context.Background(), platformcache.RDB).Allow(
			context.Background(), "desktop:invalid:attempt:"+key,
			platformratelimit.WithCapacity(desktopInvalidTokenCapacity),
			platformratelimit.WithRate(float64(desktopInvalidTokenCapacity)/desktopInvalidTokenWindow.Seconds()),
			platformratelimit.WithRequested(1),
		)
		if err != nil {
			platformobservability.SysLog(fmt.Sprintf("desktop invalid-token limiter write failed: %v", err))
			return
		}
		if !allowed {
			_ = platformcache.RDB.Set(context.Background(), "desktop:invalid:block:"+key, "1", desktopInvalidTokenWindow).Err()
		}
		return
	}

	now := time.Now()
	cutoff := now.Add(-desktopInvalidTokenWindow)
	desktopInvalidTokenMemory.Lock()
	entry := desktopInvalidTokenMemory.entries[key]
	kept := entry.Failures[:0]
	for _, failureAt := range entry.Failures {
		if failureAt.After(cutoff) {
			kept = append(kept, failureAt)
		}
	}
	entry.Failures = append(kept, now)
	if len(entry.Failures) > int(desktopInvalidTokenCapacity) {
		entry.BlockedTo = now.Add(desktopInvalidTokenWindow)
	}
	desktopInvalidTokenMemory.entries[key] = entry
	desktopInvalidTokenMemory.Unlock()
}

func writeDesktopInvalidTokenRetry(c *gin.Context) {
	c.Header("Retry-After", "60")
	c.JSON(http.StatusTooManyRequests, gin.H{
		"success": false,
		"message": "desktop access token is temporarily rate limited",
	})
	c.Abort()
}
