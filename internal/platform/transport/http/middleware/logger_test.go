package middleware

import (
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/stretchr/testify/require"
)

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
