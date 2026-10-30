package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	"github.com/stretchr/testify/require"
)

func TestReadOnlyAPIKeyMetadataRoutesDoNotReturnRateLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldRedis := platformcache.RedisEnabled
	oldGlobalEnabled := platformconfig.GlobalApiRateLimitEnable
	oldGlobalLimit := platformconfig.GlobalApiRateLimitNum
	oldGlobalDuration := platformconfig.GlobalApiRateLimitDuration
	oldCriticalEnabled := platformconfig.CriticalRateLimitEnable
	oldCriticalLimit := platformconfig.CriticalRateLimitNum
	oldCriticalDuration := platformconfig.CriticalRateLimitDuration
	platformcache.RedisEnabled = false
	platformconfig.GlobalApiRateLimitEnable = true
	platformconfig.GlobalApiRateLimitNum = 1
	platformconfig.GlobalApiRateLimitDuration = 600
	platformconfig.CriticalRateLimitEnable = true
	platformconfig.CriticalRateLimitNum = 1
	platformconfig.CriticalRateLimitDuration = 600
	t.Cleanup(func() {
		platformcache.RedisEnabled = oldRedis
		platformconfig.GlobalApiRateLimitEnable = oldGlobalEnabled
		platformconfig.GlobalApiRateLimitNum = oldGlobalLimit
		platformconfig.GlobalApiRateLimitDuration = oldGlobalDuration
		platformconfig.CriticalRateLimitEnable = oldCriticalEnabled
		platformconfig.CriticalRateLimitNum = oldCriticalLimit
		platformconfig.CriticalRateLimitDuration = oldCriticalDuration
	})

	engine := gin.New()
	registerControlAPIRoutes(engine)
	tests := []struct {
		path       string
		remoteAddr string
		wantStatus int
	}{
		{path: "/api/ratio_config", remoteAddr: "198.51.100.210:1234", wantStatus: http.StatusForbidden},
		{path: "/api/usage/token/", remoteAddr: "198.51.100.211:1234", wantStatus: http.StatusUnauthorized},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			for attempt := 0; attempt < 3; attempt++ {
				request := httptest.NewRequest(http.MethodGet, test.path, nil)
				request.RemoteAddr = test.remoteAddr
				recorder := httptest.NewRecorder()
				engine.ServeHTTP(recorder, request)
				require.Equal(t, test.wantStatus, recorder.Code, "attempt %d must not be rate limited", attempt+1)
				require.NotEqual(t, http.StatusTooManyRequests, recorder.Code)
			}
		})
	}
}
