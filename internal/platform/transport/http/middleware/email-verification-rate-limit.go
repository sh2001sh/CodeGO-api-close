package middleware

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
)

const (
	EmailVerificationRateLimitMark = "EV"
	EmailVerificationMaxRequests   = 2  // 30秒内最多2次
	EmailVerificationDuration      = 30 // 30秒时间窗口
)

var emailVerificationLimitScript = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 or redis.call('TTL', KEYS[1]) < 0 then
  redis.call('EXPIRE', KEYS[1], ARGV[1])
end
return count
`)

func redisEmailVerificationRateLimiter(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	rdb := platformcache.RDB
	key := "emailVerification:" + EmailVerificationRateLimitMark + ":" + emailVerificationClientIP(c)

	count, err := emailVerificationLimitScript.Run(ctx, rdb, []string{key}, EmailVerificationDuration).Int64()
	if err != nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "验证服务暂不可用，请稍后重试"})
		return
	}

	// 检查是否超出限制
	if count <= int64(EmailVerificationMaxRequests) {
		c.Next()
		return
	}

	// 获取剩余等待时间
	ttl, err := rdb.TTL(ctx, key).Result()
	waitSeconds := int64(EmailVerificationDuration)
	if err == nil && ttl > 0 {
		waitSeconds = int64(ttl.Seconds())
	}

	c.JSON(http.StatusTooManyRequests, gin.H{
		"success": false,
		"message": fmt.Sprintf("发送过于频繁，请等待 %d 秒后再试", waitSeconds),
	})
	c.Abort()
}

func memoryEmailVerificationRateLimiter(c *gin.Context) {
	inMemoryRateLimiter.Init(platformconfig.RateLimitKeyExpirationDuration)
	key := EmailVerificationRateLimitMark + ":" + emailVerificationClientIP(c)

	if !inMemoryRateLimiter.Request(key, EmailVerificationMaxRequests, EmailVerificationDuration) {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"success": false,
			"message": "发送过于频繁，请稍后再试",
		})
		c.Abort()
		return
	}

	c.Next()
}

func EmailVerificationRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if platformcache.RedisEnabled {
			if platformcache.RDB == nil {
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "验证服务暂不可用，请稍后重试"})
				return
			}
			redisEmailVerificationRateLimiter(c)
		} else {
			memoryEmailVerificationRateLimiter(c)
		}
	}
}

// A CDN header is authoritative only when Gin's configured trusted-proxy chain
// resolves to a Cloudflare edge. A direct caller cannot choose its own bucket.
func emailVerificationClientIP(c *gin.Context) string {
	ip := c.ClientIP()
	parsed := net.ParseIP(ip)
	for _, network := range cloudflareEmailProxyNetworks {
		if network.Contains(parsed) {
			client := net.ParseIP(c.GetHeader("CF-Connecting-IP"))
			if client != nil && client.IsGlobalUnicast() && !client.IsPrivate() && !client.IsLoopback() && !client.IsLinkLocalUnicast() {
				return client.String()
			}
			break
		}
	}
	return ip
}

// Cloudflare's published proxy ranges; other proxies continue to use Gin's
// explicit TRUSTED_PROXIES configuration and X-Forwarded-For chain validation.
var cloudflareEmailProxyNetworks = func() []*net.IPNet {
	ranges := []string{
		"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22", "141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20", "197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13", "104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
		"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32", "2405:8100::/32", "2a06:98c0::/29", "2c0f:f248::/32",
	}
	networks := make([]*net.IPNet, 0, len(ranges))
	for _, value := range ranges {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			panic(err)
		}
		networks = append(networks, network)
	}
	return networks
}()
