package middleware

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
)

const loginRateLimitScript = `
local count = redis.call("INCR", KEYS[1])
if count == 1 then
  redis.call("EXPIRE", KEYS[1], ARGV[2])
end
local ttl = redis.call("TTL", KEYS[1])
if count > tonumber(ARGV[1]) then
  return {0, ttl}
end
return {1, ttl}
`

type loginIdentifierRequest struct {
	Username string `json:"username"`
}

// LoginRateLimit isolates password-login attempts from unrelated critical
// operations and enforces both a client-IP and account-specific limit.
func LoginRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !platformconfig.LoginRateLimitEnable {
			return
		}

		allowed, retryAfter := allowLoginAttempt(
			c,
			"rateLimit:LI:ip:"+c.ClientIP(),
			platformconfig.LoginIPRateLimitNum,
		)
		if !allowed {
			writeLoginRateLimitResponse(c, retryAfter)
			return
		}

		account := loginAccountFromRequest(c)
		if account == "" {
			return
		}
		allowed, retryAfter = allowLoginAttempt(
			c,
			"rateLimit:LI:account:"+hashLoginAccount(account),
			platformconfig.LoginAccountRateLimitNum,
		)
		if !allowed {
			writeLoginRateLimitResponse(c, retryAfter)
		}
	}
}

func allowLoginAttempt(c *gin.Context, key string, limit int) (bool, int64) {
	duration := platformconfig.LoginRateLimitDuration
	if limit <= 0 || duration <= 0 {
		return true, 0
	}

	if platformcache.RedisEnabled && platformcache.RDB != nil {
		result, err := platformcache.RDB.Eval(
			c.Request.Context(),
			loginRateLimitScript,
			[]string{key},
			limit,
			duration,
		).Int64Slice()
		if err == nil && len(result) == 2 {
			return result[0] == 1, normalizeRetryAfter(result[1], duration)
		}
		platformobservability.SysLog(fmt.Sprintf("login rate limiter unavailable, using memory fallback: %v", err))
	}

	inMemoryRateLimiter.Init(platformconfig.RateLimitKeyExpirationDuration)
	return inMemoryRateLimiter.RequestWithRetryAfter(key, limit, duration)
}

func loginAccountFromRequest(c *gin.Context) string {
	if c.Request == nil || c.Request.Body == nil {
		return ""
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return ""
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	c.Request.ContentLength = int64(len(body))

	var request loginIdentifierRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(request.Username))
}

func hashLoginAccount(account string) string {
	digest := sha256.Sum256([]byte(account))
	return hex.EncodeToString(digest[:])
}

func normalizeRetryAfter(retryAfter int64, fallback int64) int64 {
	if retryAfter < 1 {
		if fallback > 0 {
			return fallback
		}
		return 1
	}
	return retryAfter
}

func writeLoginRateLimitResponse(c *gin.Context, retryAfter int64) {
	retryAfter = normalizeRetryAfter(retryAfter, platformconfig.LoginRateLimitDuration)
	c.Header("Retry-After", strconv.FormatInt(retryAfter, 10))
	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
		"success":     false,
		"code":        "LOGIN_RATE_LIMITED",
		"message":     fmt.Sprintf("登录尝试过于频繁，请在 %d 秒后重试", retryAfter),
		"retry_after": retryAfter,
	})
}
