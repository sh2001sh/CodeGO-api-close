package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	"github.com/stretchr/testify/require"
)

func TestBlindBoxOpenRateLimitIsScopedToUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalRedisEnabled := platformcache.RedisEnabled
	platformcache.RedisEnabled = false
	t.Cleanup(func() {
		platformcache.RedisEnabled = originalRedisEnabled
	})

	limiter := BlindBoxOpenRateLimit()
	for attempt := 0; attempt < 30; attempt++ {
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
