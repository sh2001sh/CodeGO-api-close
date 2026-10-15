package middleware

import (
	"fmt"
	"github.com/sh2001sh/new-api/constant"

	"github.com/gin-gonic/gin"
)

const RouteTagKey = "route_tag"

func RouteTag(tag string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(RouteTagKey, tag)
		c.Next()
	}
}

func SetUpLogger(server *gin.Engine) {
	server.Use(gin.LoggerWithFormatter(formatRequestLog))
}

func formatRequestLog(param gin.LogFormatterParams) string {
	var requestID string
	var traceID string
	if param.Keys != nil {
		requestID, _ = param.Keys[constant.RequestIdKey].(string)
		traceID, _ = param.Keys[constant.TraceIdKey].(string)
	}
	tag, _ := param.Keys[RouteTagKey].(string)
	if tag == "" {
		tag = "web"
	}
	return fmt.Sprintf("[GIN] %s | %s | %s | %3d | %13v | %15s | %7s %s | trace=%s\n",
		param.TimeStamp.Format("2006/01/02 - 15:04:05"),
		tag,
		requestID,
		param.StatusCode,
		param.Latency,
		param.ClientIP,
		param.Method,
		param.Path,
		logTraceID(traceID),
	)
}

// The trace header may come from a client. Keep it bounded and single-line.
func logTraceID(traceID string) string {
	if len(traceID) == 0 || len(traceID) > 128 {
		return "-"
	}
	for _, c := range traceID {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == ':' || c == '.') {
			return "-"
		}
	}
	return traceID
}
