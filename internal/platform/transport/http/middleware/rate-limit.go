package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformratelimit "github.com/sh2001sh/new-api/internal/platform/ratelimit"
)

var timeFormat = "2006-01-02T15:04:05.000Z"

var inMemoryRateLimiter InMemoryRateLimiter

var defNext = func(c *gin.Context) {
	c.Next()
}

func redisRateLimiter(c *gin.Context, maxRequestNum int, duration int64, mark string) {
	ctx := context.Background()
	rdb := platformcache.RDB
	key := "rateLimit:" + mark + c.ClientIP()
	listLength, err := rdb.LLen(ctx, key).Result()
	if err != nil {
		platformobservability.SysLog(fmt.Sprintf("redis rate limiter unavailable: %v", err))
		return
	}
	if listLength < int64(maxRequestNum) {
		rdb.LPush(ctx, key, time.Now().Format(timeFormat))
		rdb.Expire(ctx, key, rateLimitKeyExpiration(duration))
	} else {
		oldTimeStr, _ := rdb.LIndex(ctx, key, -1).Result()
		oldTime, err := time.Parse(timeFormat, oldTimeStr)
		if err != nil {
			platformobservability.SysLog(fmt.Sprintf("redis rate limiter timestamp invalid: %v", err))
			return
		}
		nowTimeStr := time.Now().Format(timeFormat)
		nowTime, err := time.Parse(timeFormat, nowTimeStr)
		if err != nil {
			platformobservability.SysLog(fmt.Sprintf("redis rate limiter clock parse failed: %v", err))
			return
		}
		// time.Since will return negative number!
		// See: https://stackoverflow.com/questions/50970900/why-is-time-since-returning-negative-durations-on-windows
		if int64(nowTime.Sub(oldTime).Seconds()) < duration {
			rdb.Expire(ctx, key, rateLimitKeyExpiration(duration))
			c.Status(http.StatusTooManyRequests)
			c.Abort()
			return
		} else {
			rdb.LPush(ctx, key, time.Now().Format(timeFormat))
			rdb.LTrim(ctx, key, 0, int64(maxRequestNum-1))
			rdb.Expire(ctx, key, rateLimitKeyExpiration(duration))
		}
	}
}

func rateLimitKeyExpiration(duration int64) time.Duration {
	window := time.Duration(duration) * time.Second
	if window > platformconfig.RateLimitKeyExpirationDuration {
		return window
	}
	return platformconfig.RateLimitKeyExpirationDuration
}

func memoryRateLimiter(c *gin.Context, maxRequestNum int, duration int64, mark string) {
	key := mark + c.ClientIP()
	if !inMemoryRateLimiter.Request(key, maxRequestNum, duration) {
		c.Status(http.StatusTooManyRequests)
		c.Abort()
		return
	}
}

func rateLimitFactory(maxRequestNum int, duration int64, mark string) func(c *gin.Context) {
	if platformcache.RedisEnabled && platformcache.RDB != nil {
		return func(c *gin.Context) {
			redisRateLimiter(c, maxRequestNum, duration, mark)
		}
	} else {
		// It's safe to call multi times.
		inMemoryRateLimiter.Init(platformconfig.RateLimitKeyExpirationDuration)
		return func(c *gin.Context) {
			memoryRateLimiter(c, maxRequestNum, duration, mark)
		}
	}
}

func GlobalWebRateLimit() func(c *gin.Context) {
	if platformconfig.GlobalWebRateLimitEnable {
		return rateLimitFactory(platformconfig.GlobalWebRateLimitNum, platformconfig.GlobalWebRateLimitDuration, "GW")
	}
	return defNext
}

func GlobalAPIRateLimit() func(c *gin.Context) {
	if platformconfig.GlobalApiRateLimitEnable {
		return rateLimitFactory(platformconfig.GlobalApiRateLimitNum, platformconfig.GlobalApiRateLimitDuration, "GA")
	}
	return defNext
}

// GlobalAuthenticatedAPIRateLimit isolates authenticated traffic by user or
// desktop device so one noisy client IP cannot consume another user's quota.
func GlobalAuthenticatedAPIRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !enforceGlobalAuthenticatedAPIRateLimit(c) {
			return
		}
		c.Next()
	}
}

func enforceGlobalAuthenticatedAPIRateLimit(c *gin.Context) bool {
	if !platformconfig.GlobalApiRateLimitEnable {
		return true
	}

	key := globalAuthenticatedRateLimitKey(c)
	if key == "" {
		return true
	}
	if platformcache.RedisEnabled && platformcache.RDB != nil {
		allowed, err := platformratelimit.NewRedisLimiter(c.Request.Context(), platformcache.RDB).Allow(
			c.Request.Context(),
			"rateLimit:GA:"+key,
			platformratelimit.WithCapacity(int64(platformconfig.GlobalApiRateLimitNum)),
			platformratelimit.WithRate(globalAPIRatePerSecond()),
			platformratelimit.WithRequested(1),
		)
		if err != nil {
			platformobservability.SysLog(fmt.Sprintf("authenticated API rate limiter unavailable: %v", err))
			return true
		}
		if !allowed {
			c.Status(http.StatusTooManyRequests)
			c.Abort()
			return false
		}
		return true
	}

	if !inMemoryRateLimiter.Request("GA:"+key, platformconfig.GlobalApiRateLimitNum, platformconfig.GlobalApiRateLimitDuration) {
		c.Status(http.StatusTooManyRequests)
		c.Abort()
		return false
	}
	return true
}

func globalAPIRatePerSecond() float64 {
	if platformconfig.GlobalApiRateLimitDuration <= 0 {
		return 1
	}
	return float64(platformconfig.GlobalApiRateLimitNum) / float64(platformconfig.GlobalApiRateLimitDuration)
}

func globalAuthenticatedRateLimitKey(c *gin.Context) string {
	if desktopDeviceID := c.GetInt("desktop_device_id"); desktopDeviceID > 0 {
		return fmt.Sprintf("desktop:%d", desktopDeviceID)
	}
	if userID := c.GetInt("id"); userID > 0 {
		return fmt.Sprintf("user:%d", userID)
	}
	return ""
}

func CriticalRateLimit() func(c *gin.Context) {
	if !platformconfig.CriticalRateLimitEnable {
		return defNext
	}

	ipLimiter := rateLimitFactory(platformconfig.CriticalRateLimitNum, platformconfig.CriticalRateLimitDuration, "CT")
	userLimiter := userRateLimitFactory(platformconfig.CriticalRateLimitNum, platformconfig.CriticalRateLimitDuration, "CTU")
	return func(c *gin.Context) {
		if c.GetInt("id") > 0 {
			userLimiter(c)
			return
		}
		ipLimiter(c)
	}
}

// BlindBoxOpenRateLimit limits opening by authenticated user rather than the
// shared client IP. Opening a batch one at a time is an expected workflow, so
// a user must be able to reveal a large granted batch without waiting for a
// shared window to expire. The transaction layer remains the authority for
// stock consumption and reward issuance.
func BlindBoxOpenRateLimit() func(c *gin.Context) {
	return userRateLimitFactory(120, 60, "BBO")
}

// BalanceBlindBoxOpenRateLimit limits balance blind-box reveals per
// authenticated user. It is intentionally separate from regular blind-box
// reveals and payment creation so a shared network cannot exhaust another
// user's allowance.
func BalanceBlindBoxOpenRateLimit() func(c *gin.Context) {
	return userRateLimitFactory(60, 60, "BBOB")
}

// BlindBoxPaymentRateLimit prevents duplicate payment creation while keeping
// the limit scoped to the authenticated user rather than a shared client IP.
func BlindBoxPaymentRateLimit() func(c *gin.Context) {
	return userRateLimitFactory(10, 60, "BBP")
}

// RegistrationRateLimit allows up to five account-creation attempts from the
// same IP in a rolling ten-minute window.
func RegistrationRateLimit() gin.HandlerFunc {
	return rateLimitFactory(5, 10*60, "RG10")
}

func DownloadRateLimit() func(c *gin.Context) {
	return rateLimitFactory(platformconfig.DownloadRateLimitNum, platformconfig.DownloadRateLimitDuration, "DW")
}

func UploadRateLimit() func(c *gin.Context) {
	return rateLimitFactory(platformconfig.UploadRateLimitNum, platformconfig.UploadRateLimitDuration, "UP")
}

// userRateLimitFactory creates a rate limiter keyed by authenticated user ID
// instead of client IP, making it resistant to proxy rotation attacks.
// Must be used AFTER authentication middleware (UserAuth).
func userRateLimitFactory(maxRequestNum int, duration int64, mark string) func(c *gin.Context) {
	if platformcache.RedisEnabled && platformcache.RDB != nil {
		return func(c *gin.Context) {
			userId := c.GetInt("id")
			if userId == 0 {
				c.Status(http.StatusUnauthorized)
				c.Abort()
				return
			}
			key := fmt.Sprintf("rateLimit:%s:user:%d", mark, userId)
			userRedisRateLimiter(c, maxRequestNum, duration, key)
		}
	}
	// It's safe to call multi times.
	inMemoryRateLimiter.Init(platformconfig.RateLimitKeyExpirationDuration)
	return func(c *gin.Context) {
		userId := c.GetInt("id")
		if userId == 0 {
			c.Status(http.StatusUnauthorized)
			c.Abort()
			return
		}
		key := fmt.Sprintf("%s:user:%d", mark, userId)
		if !inMemoryRateLimiter.Request(key, maxRequestNum, duration) {
			c.Status(http.StatusTooManyRequests)
			c.Abort()
			return
		}
	}
}

// userRedisRateLimiter is like redisRateLimiter but accepts a pre-built key
// (to support user-ID-based keys).
func userRedisRateLimiter(c *gin.Context, maxRequestNum int, duration int64, key string) {
	ctx := context.Background()
	rdb := platformcache.RDB
	listLength, err := rdb.LLen(ctx, key).Result()
	if err != nil {
		platformobservability.SysLog(fmt.Sprintf("redis user rate limiter unavailable: %v", err))
		return
	}
	if listLength < int64(maxRequestNum) {
		rdb.LPush(ctx, key, time.Now().Format(timeFormat))
		rdb.Expire(ctx, key, platformconfig.RateLimitKeyExpirationDuration)
	} else {
		oldTimeStr, _ := rdb.LIndex(ctx, key, -1).Result()
		oldTime, err := time.Parse(timeFormat, oldTimeStr)
		if err != nil {
			platformobservability.SysLog(fmt.Sprintf("redis user rate limiter timestamp invalid: %v", err))
			return
		}
		nowTimeStr := time.Now().Format(timeFormat)
		nowTime, err := time.Parse(timeFormat, nowTimeStr)
		if err != nil {
			platformobservability.SysLog(fmt.Sprintf("redis user rate limiter clock parse failed: %v", err))
			return
		}
		if int64(nowTime.Sub(oldTime).Seconds()) < duration {
			rdb.Expire(ctx, key, platformconfig.RateLimitKeyExpirationDuration)
			c.Status(http.StatusTooManyRequests)
			c.Abort()
			return
		} else {
			rdb.LPush(ctx, key, time.Now().Format(timeFormat))
			rdb.LTrim(ctx, key, 0, int64(maxRequestNum-1))
			rdb.Expire(ctx, key, platformconfig.RateLimitKeyExpirationDuration)
		}
	}
}

// SearchRateLimit returns a per-user rate limiter for search endpoints.
// Configurable via SEARCH_RATE_LIMIT_ENABLE / SEARCH_RATE_LIMIT / SEARCH_RATE_LIMIT_DURATION.
func SearchRateLimit() func(c *gin.Context) {
	if !platformconfig.SearchRateLimitEnable {
		return defNext
	}
	return userRateLimitFactory(platformconfig.SearchRateLimitNum, platformconfig.SearchRateLimitDuration, "SR")
}
