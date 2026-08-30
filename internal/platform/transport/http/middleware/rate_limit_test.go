package middleware

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	"github.com/stretchr/testify/require"
)

func TestLoginRateLimitIsIndependentFromCriticalBucket(t *testing.T) {
	restore := configureMemoryLoginRateLimit(t, 3, 2, 600)
	defer restore()

	criticalLimit := platformconfig.CriticalRateLimitNum
	criticalDuration := platformconfig.CriticalRateLimitDuration
	criticalEnabled := platformconfig.CriticalRateLimitEnable
	platformconfig.CriticalRateLimitEnable = true
	platformconfig.CriticalRateLimitNum = 1
	platformconfig.CriticalRateLimitDuration = 600
	defer func() {
		platformconfig.CriticalRateLimitNum = criticalLimit
		platformconfig.CriticalRateLimitDuration = criticalDuration
		platformconfig.CriticalRateLimitEnable = criticalEnabled
	}()

	critical := CriticalRateLimit()
	firstCritical := newRateLimitContext(http.MethodPost, "/api/wallet/transfers", "198.51.100.80", "")
	critical(firstCritical)
	require.False(t, firstCritical.IsAborted())
	limitedCritical := newRateLimitContext(http.MethodPost, "/api/wallet/transfers", "198.51.100.80", "")
	critical(limitedCritical)
	require.True(t, limitedCritical.IsAborted())

	login := LoginRateLimit()
	loginContext := newRateLimitContext(http.MethodPost, "/api/user/login", "198.51.100.80", `{"username":"isolated-user","password":"secret"}`)
	login(loginContext)
	require.False(t, loginContext.IsAborted())
}

func TestLoginRateLimitUsesNormalizedAccountAcrossIPsAndPreservesBody(t *testing.T) {
	restore := configureMemoryLoginRateLimit(t, 100, 2, 600)
	defer restore()

	limiter := LoginRateLimit()
	firstBody := `{"username":"  User@Example.COM ","password":"first"}`
	first := newRateLimitContext(http.MethodPost, "/api/user/login", "198.51.100.81", firstBody)
	limiter(first)
	require.False(t, first.IsAborted())
	preservedBody, err := io.ReadAll(first.Request.Body)
	require.NoError(t, err)
	require.JSONEq(t, firstBody, string(preservedBody))

	second := newRateLimitContext(http.MethodPost, "/api/user/login", "198.51.100.82", `{"username":"user@example.com","password":"second"}`)
	limiter(second)
	require.False(t, second.IsAborted())

	limitedRecorder := httptest.NewRecorder()
	limited, _ := gin.CreateTestContext(limitedRecorder)
	limited.Request = httptest.NewRequest(http.MethodPost, "/api/user/login", strings.NewReader(`{"username":"USER@EXAMPLE.COM","password":"third"}`))
	limited.Request.RemoteAddr = "198.51.100.83:1234"
	limiter(limited)
	require.True(t, limited.IsAborted())
	require.Equal(t, http.StatusTooManyRequests, limited.Writer.Status())
	require.NotEmpty(t, limited.Writer.Header().Get("Retry-After"))

	var response map[string]any
	require.NoError(t, json.Unmarshal(limitedRecorder.Body.Bytes(), &response))
	require.Equal(t, "LOGIN_RATE_LIMITED", response["code"])
	require.NotZero(t, response["retry_after"])
}

func TestLoginRateLimitUsesIPBucketAcrossAccounts(t *testing.T) {
	restore := configureMemoryLoginRateLimit(t, 2, 100, 600)
	defer restore()

	limiter := LoginRateLimit()
	for _, username := range []string{"ip-user-one", "ip-user-two"} {
		context := newRateLimitContext(http.MethodPost, "/api/user/login", "198.51.100.84", `{"username":"`+username+`","password":"secret"}`)
		limiter(context)
		require.False(t, context.IsAborted())
	}

	limited := newRateLimitContext(http.MethodPost, "/api/user/login", "198.51.100.84", `{"username":"ip-user-three","password":"secret"}`)
	limiter(limited)
	require.True(t, limited.IsAborted())
	require.Equal(t, http.StatusTooManyRequests, limited.Writer.Status())
}

func TestCriticalRateLimitScopesAuthenticatedRequestsToUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalRedisEnabled := platformcache.RedisEnabled
	originalEnabled := platformconfig.CriticalRateLimitEnable
	originalLimit := platformconfig.CriticalRateLimitNum
	originalDuration := platformconfig.CriticalRateLimitDuration
	platformcache.RedisEnabled = false
	platformconfig.CriticalRateLimitEnable = true
	platformconfig.CriticalRateLimitNum = 1
	platformconfig.CriticalRateLimitDuration = 600
	defer func() {
		platformcache.RedisEnabled = originalRedisEnabled
		platformconfig.CriticalRateLimitEnable = originalEnabled
		platformconfig.CriticalRateLimitNum = originalLimit
		platformconfig.CriticalRateLimitDuration = originalDuration
	}()

	limiter := CriticalRateLimit()
	firstUser := newRateLimitContext(http.MethodPost, "/api/wallet/transfers", "198.51.100.85", "")
	firstUser.Set("id", 5101)
	limiter(firstUser)
	require.False(t, firstUser.IsAborted())

	otherUser := newRateLimitContext(http.MethodPost, "/api/wallet/transfers", "198.51.100.85", "")
	otherUser.Set("id", 5102)
	limiter(otherUser)
	require.False(t, otherUser.IsAborted())

	limited := newRateLimitContext(http.MethodPost, "/api/wallet/transfers", "203.0.113.85", "")
	limited.Set("id", 5101)
	limiter(limited)
	require.True(t, limited.IsAborted())
}

func TestAuthenticatedRateLimitReturnsRetryMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalRedisEnabled := platformcache.RedisEnabled
	originalEnabled := platformconfig.GlobalApiRateLimitEnable
	originalLimit := platformconfig.GlobalApiRateLimitNum
	originalDuration := platformconfig.GlobalApiRateLimitDuration
	platformcache.RedisEnabled = false
	platformconfig.GlobalApiRateLimitEnable = true
	platformconfig.GlobalApiRateLimitNum = 1
	platformconfig.GlobalApiRateLimitDuration = 600
	t.Cleanup(func() {
		platformcache.RedisEnabled = originalRedisEnabled
		platformconfig.GlobalApiRateLimitEnable = originalEnabled
		platformconfig.GlobalApiRateLimitNum = originalLimit
		platformconfig.GlobalApiRateLimitDuration = originalDuration
	})

	first := newRateLimitContext(http.MethodGet, "/api/desktop/account/summary", "198.51.100.90", "")
	first.Set("desktop_device_id", 99001)
	require.True(t, enforceGlobalAuthenticatedAPIRateLimit(first))

	recorder := httptest.NewRecorder()
	limited, _ := gin.CreateTestContext(recorder)
	limited.Request = httptest.NewRequest(http.MethodGet, "/api/desktop/account/summary", nil)
	limited.Set("desktop_device_id", 99001)
	require.False(t, enforceGlobalAuthenticatedAPIRateLimit(limited))
	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.Equal(t, "1", recorder.Header().Get("Retry-After"))

	var response map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "RATE_LIMITED", response["code"])
	require.Equal(t, float64(1), response["retry_after"])
}

func TestDesktopAuthenticatedRateLimitUsesIndependentReadScopes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	paths := map[string]string{
		"/api/desktop/account/summary": "account",
		"/api/desktop/usage/trends":    "usage",
		"/api/desktop/pricing":         "routing",
		"/api/desktop/group-status":    "routing",
		"/api/desktop/tokens":          "configuration",
	}
	for path, scope := range paths {
		context := newRateLimitContext(http.MethodGet, path, "198.51.100.91", "")
		context.Set("desktop_device_id", 99002)
		require.Equal(t, "desktop:99002:"+scope, globalAuthenticatedRateLimitKey(context))
	}
}

func configureMemoryLoginRateLimit(t *testing.T, ipLimit int, accountLimit int, duration int64) func() {
	t.Helper()
	gin.SetMode(gin.TestMode)
	originalRedisEnabled := platformcache.RedisEnabled
	originalEnabled := platformconfig.LoginRateLimitEnable
	originalIPLimit := platformconfig.LoginIPRateLimitNum
	originalAccountLimit := platformconfig.LoginAccountRateLimitNum
	originalDuration := platformconfig.LoginRateLimitDuration
	platformcache.RedisEnabled = false
	platformconfig.LoginRateLimitEnable = true
	platformconfig.LoginIPRateLimitNum = ipLimit
	platformconfig.LoginAccountRateLimitNum = accountLimit
	platformconfig.LoginRateLimitDuration = duration
	return func() {
		platformcache.RedisEnabled = originalRedisEnabled
		platformconfig.LoginRateLimitEnable = originalEnabled
		platformconfig.LoginIPRateLimitNum = originalIPLimit
		platformconfig.LoginAccountRateLimitNum = originalAccountLimit
		platformconfig.LoginRateLimitDuration = originalDuration
	}
}

func newRateLimitContext(method string, path string, ip string, body string) *gin.Context {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(method, path, strings.NewReader(body))
	context.Request.RemoteAddr = ip + ":1234"
	return context
}

func TestBlindBoxOpenRateLimitIsScopedToUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalRedisEnabled := platformcache.RedisEnabled
	platformcache.RedisEnabled = false
	t.Cleanup(func() {
		platformcache.RedisEnabled = originalRedisEnabled
	})

	limiter := BlindBoxOpenRateLimit()
	for attempt := 0; attempt < 120; attempt++ {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = httptest.NewRequest(http.MethodPost, "/api/blind-box/open", nil)
		context.Set("id", 901)
		limiter(context)
		require.False(t, context.IsAborted())
	}

	limited, _ := gin.CreateTestContext(httptest.NewRecorder())
	limited.Request = httptest.NewRequest(http.MethodPost, "/api/blind-box/open", nil)
	limited.Set("id", 901)
	limiter(limited)
	require.True(t, limited.IsAborted())
	require.Equal(t, http.StatusTooManyRequests, limited.Writer.Status())

	otherUser, _ := gin.CreateTestContext(httptest.NewRecorder())
	otherUser.Request = httptest.NewRequest(http.MethodPost, "/api/blind-box/open", nil)
	otherUser.Set("id", 902)
	limiter(otherUser)
	require.False(t, otherUser.IsAborted())
}

func TestRegistrationRateLimitAllowsFiveAttemptsPerTenMinutesPerIP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalRedisEnabled := platformcache.RedisEnabled
	platformcache.RedisEnabled = false
	t.Cleanup(func() { platformcache.RedisEnabled = originalRedisEnabled })

	limiter := RegistrationRateLimit()
	request := func(ip string) *gin.Context {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = httptest.NewRequest(http.MethodPost, "/api/user/register", nil)
		context.Request.RemoteAddr = ip + ":1234"
		limiter(context)
		return context
	}

	for range 5 {
		allowed := request("198.51.100.41")
		require.False(t, allowed.IsAborted())
	}
	limited := request("198.51.100.41")
	require.True(t, limited.IsAborted())
	require.Equal(t, http.StatusTooManyRequests, limited.Writer.Status())
	otherIP := request("198.51.100.42")
	require.False(t, otherIP.IsAborted())
}

func TestBalanceBlindBoxOpenRateLimitIsScopedToUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalRedisEnabled := platformcache.RedisEnabled
	platformcache.RedisEnabled = false
	t.Cleanup(func() {
		platformcache.RedisEnabled = originalRedisEnabled
	})

	limiter := BalanceBlindBoxOpenRateLimit()
	for attempt := 0; attempt < 60; attempt++ {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = httptest.NewRequest(http.MethodPost, "/api/blind-box/balance/open", nil)
		context.Set("id", 1901)
		limiter(context)
		require.False(t, context.IsAborted())
	}

	limited, _ := gin.CreateTestContext(httptest.NewRecorder())
	limited.Request = httptest.NewRequest(http.MethodPost, "/api/blind-box/balance/open", nil)
	limited.Set("id", 1901)
	limiter(limited)
	require.True(t, limited.IsAborted())
	require.Equal(t, http.StatusTooManyRequests, limited.Writer.Status())

	otherUser, _ := gin.CreateTestContext(httptest.NewRecorder())
	otherUser.Request = httptest.NewRequest(http.MethodPost, "/api/blind-box/balance/open", nil)
	otherUser.Set("id", 1902)
	limiter(otherUser)
	require.False(t, otherUser.IsAborted())
}

func TestBlindBoxPaymentRateLimitUsesSeparateUserBucket(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalRedisEnabled := platformcache.RedisEnabled
	platformcache.RedisEnabled = false
	t.Cleanup(func() {
		platformcache.RedisEnabled = originalRedisEnabled
	})

	paymentLimiter := BlindBoxPaymentRateLimit()
	for attempt := 0; attempt < 10; attempt++ {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = httptest.NewRequest(http.MethodPost, "/api/blind-box/pay", nil)
		context.Set("id", 2901)
		paymentLimiter(context)
		require.False(t, context.IsAborted())
	}

	limited, _ := gin.CreateTestContext(httptest.NewRecorder())
	limited.Request = httptest.NewRequest(http.MethodPost, "/api/blind-box/pay", nil)
	limited.Set("id", 2901)
	paymentLimiter(limited)
	require.True(t, limited.IsAborted())
	require.Equal(t, http.StatusTooManyRequests, limited.Writer.Status())

	balanceLimiter := BalanceBlindBoxOpenRateLimit()
	balanceContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	balanceContext.Request = httptest.NewRequest(http.MethodPost, "/api/blind-box/balance/open", nil)
	balanceContext.Set("id", 2901)
	balanceLimiter(balanceContext)
	require.False(t, balanceContext.IsAborted())
}
