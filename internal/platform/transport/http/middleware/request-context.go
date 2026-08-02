package middleware

import (
	"fmt"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"strings"

	"github.com/gin-gonic/gin"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
)

var heavyGlobalAPIRateLimitedRequests = map[string]struct{}{
	"GET /api/user/self":                            {},
	"GET /api/user/self/groups":                     {},
	"GET /api/user/self/group-status":               {},
	"GET /api/user/topup/info":                      {},
	"GET /api/user/topup/self":                      {},
	"GET /api/subscription/self":                    {},
	"GET /api/subscription/self/claude-conversions": {},
	"POST /api/user/amount":                         {},
	"POST /api/user/stripe/amount":                  {},
	"POST /api/user/waffo/amount":                   {},
	"POST /api/user/waffo-pancake/amount":           {},
	"POST /api/blind-box/amount":                    {},
}

func IsHeavyGlobalAPIRateLimitedRequest(method, path string) bool {
	_, ok := heavyGlobalAPIRateLimitedRequests[fmt.Sprintf("%s %s", method, path)]
	return ok
}

func GlobalAPIRateLimitExceptReadPaths() gin.HandlerFunc {
	return func(c *gin.Context) {
		if isAuthenticatedAPIRoute(c.Request.Method, c.Request.URL.Path) {
			c.Next()
			return
		}
		if IsHeavyGlobalAPIRateLimitedRequest(c.Request.Method, c.Request.URL.Path) {
			c.Next()
			return
		}
		GlobalAPIRateLimit()(c)
	}
}

func isAuthenticatedAPIRoute(method, path string) bool {
	if strings.HasPrefix(path, "/api/desktop/") {
		return path != "/api/desktop/import/config" &&
			path != "/api/desktop/release/latest" &&
			path != "/api/desktop/release/latest.json"
	}
	if strings.HasPrefix(path, "/api/token") ||
		strings.HasPrefix(path, "/api/subscription") ||
		strings.HasPrefix(path, "/api/packages") ||
		strings.HasPrefix(path, "/api/group-buy") ||
		strings.HasPrefix(path, "/api/daily-lucky-number") ||
		strings.HasPrefix(path, "/api/blind-box/admin") ||
		strings.HasPrefix(path, "/api/admin/") ||
		strings.HasPrefix(path, "/api/log") {
		return true
	}
	if path == "/api/bounties" || strings.HasPrefix(path, "/api/bounties/") {
		return strings.Contains(path, "/mine") ||
			strings.Contains(path, "/balances") ||
			strings.Contains(path, "/notifications") ||
			method != "GET"
	}
	return strings.HasPrefix(path, "/api/user/self") ||
		strings.HasPrefix(path, "/api/user/models") ||
		strings.HasPrefix(path, "/api/user/image-workspace") ||
		strings.HasPrefix(path, "/api/user/topup") ||
		strings.HasPrefix(path, "/api/user/pay") ||
		strings.HasPrefix(path, "/api/user/xunhu/pay") ||
		strings.HasPrefix(path, "/api/user/stripe") ||
		strings.HasPrefix(path, "/api/user/creem") ||
		strings.HasPrefix(path, "/api/user/waffo") ||
		strings.HasPrefix(path, "/api/user/aff") ||
		strings.HasPrefix(path, "/api/user/setting") ||
		strings.HasPrefix(path, "/api/user/passkey") ||
		strings.HasPrefix(path, "/api/user/2fa") ||
		strings.HasPrefix(path, "/api/user/checkin") ||
		strings.HasPrefix(path, "/api/user/oauth/bindings") ||
		strings.HasPrefix(path, "/api/user/miniprogram")
}

func ConfigureTrustedProxies(router *gin.Engine) {
	if router == nil {
		return
	}
	if len(platformconfig.TrustedProxies) == 0 {
		if err := router.SetTrustedProxies(nil); err != nil {
			platformobservability.SysError("failed to clear trusted proxies: " + err.Error())
		}
		return
	}
	if err := router.SetTrustedProxies(platformconfig.TrustedProxies); err != nil {
		platformobservability.SysError("failed to set trusted proxies: " + err.Error())
	}
	if len(platformconfig.TrustedProxies) > 0 {
		router.ForwardedByClientIP = true
		router.RemoteIPHeaders = []string{"X-Forwarded-For", "X-Real-IP"}
	}
}
