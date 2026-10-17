package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/internal/platform/transport/http/middleware"
	"github.com/stretchr/testify/require"
)

func TestXunhuNotifyRejectsOversizedAndChunkedBodies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldLimit := constant.AnonymousRequestBodyLimitKB
	constant.AnonymousRequestBodyLimitKB = 1
	t.Cleanup(func() { constant.AnonymousRequestBodyLimitKB = oldLimit })
	engine := gin.New()
	RegisterUserRoutes(engine.Group("/api"), middleware.AnonymousRequestBodyLimit())
	for _, length := range []int64{1025, -1} {
		r := httptest.NewRequest(http.MethodPost, "/api/user/xunhu/notify", strings.NewReader(strings.Repeat("x", 1025)))
		r.ContentLength = length
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, r)
		require.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	}
	// A malformed, bounded notification reaches normal signature rejection.
	r := httptest.NewRequest(http.MethodPost, "/api/user/xunhu/notify", strings.NewReader("invalid=1"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "fail", w.Body.String())
}
