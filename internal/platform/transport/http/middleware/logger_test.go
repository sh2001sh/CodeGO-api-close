package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestRequestLoggerOmitsCapabilityQueries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		path   string
		status int
	}{
		{"/api/desktop/import/config?code=fixture-import-capability", http.StatusOK},
		{"/api/desktop/import/config/?code=fixture-import-capability", http.StatusTooManyRequests},
		{"/api/desktop/import/%63onfig?code=fixture-import-capability", http.StatusNotFound},
		{"/v1/files/file-codego-fixture/delivery?expires=9999999999&signature=fixture-file-capability", http.StatusOK},
		{"/v1/files/file-codego-fixture/delivery?signature=fixture-file-capability", http.StatusNotFound},
	} {
		t.Run(tc.path, func(t *testing.T) {
			var output bytes.Buffer
			router := gin.New()
			router.Use(gin.LoggerWithConfig(gin.LoggerConfig{Formatter: formatRequestLog, Output: &output}))
			router.NoRoute(func(c *gin.Context) { c.Status(tc.status) })
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, tc.path, nil)
			router.ServeHTTP(recorder, request)
			require.Equal(t, tc.status, recorder.Code)
			line := output.String()
			require.Contains(t, line, request.URL.Path)
			require.NotContains(t, line, "?")
			require.NotContains(t, line, "capability")
		})
	}
	line := formatRequestLog(gin.LogFormatterParams{Path: "/api/status?view=public"})
	require.Contains(t, line, "/api/status?view=public")
}

func TestRequestLogIncludesCorrelatableTrace(t *testing.T) {
	trace := "0123456789abcdef0123456789abcdef"
	line := formatRequestLog(gin.LogFormatterParams{
		TimeStamp:  time.Date(2026, 10, 10, 4, 40, 24, 0, time.UTC),
		StatusCode: 499, Method: "POST", Path: "/v1/chat/completions", Latency: 125 * time.Second,
		Keys: map[any]any{constant.RequestIdKey: "gateway-request", constant.TraceIdKey: trace, RouteTagKey: "gateway"},
	})
	require.Contains(t, line, " | gateway | gateway-request | 499 |")
	require.Contains(t, line, "/v1/chat/completions | trace="+trace+"\n")
	require.Equal(t, 1, strings.Count(line, "\n"))
}

func TestRequestLogRejectsUnsafeOrOversizedTrace(t *testing.T) {
	for _, trace := range []string{"", "trace\nforged-entry", "trace\rentry", "trace | forged", "trace\x00entry", strings.Repeat("a", 129), "跟踪"} {
		t.Run(strings.ReplaceAll(trace, "\n", "newline"), func(t *testing.T) {
			line := formatRequestLog(gin.LogFormatterParams{Keys: map[any]any{constant.TraceIdKey: trace}})
			require.True(t, strings.HasSuffix(line, " | trace=-\n"))
			require.Equal(t, 1, strings.Count(line, "\n"))
		})
	}
	require.Equal(t, strings.Repeat("a", 128), logTraceID(strings.Repeat("a", 128)))
	require.Equal(t, "request-1_test:part.2", logTraceID("request-1_test:part.2"))
}
