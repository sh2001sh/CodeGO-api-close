package runtime

import "github.com/gin-gonic/gin"

const (
	localStreamMaxDurationContextKey = "local_stream_max_duration_exceeded"
	localStreamTimeoutReasonKey      = "local_stream_timeout_reason"

	LocalStreamTimeoutFirstByte        = "first_byte"
	LocalStreamTimeoutIdle             = "idle"
	LocalStreamTimeoutAdaptiveInitial  = "adaptive_initial"
	LocalStreamTimeoutAdaptiveProgress = "adaptive_progress"
	LocalStreamTimeoutMaxDuration      = "max_duration"
)

// MarkLocalStreamMaxDurationExceeded identifies a gateway-enforced total
// stream duration expiry. It is distinct from an idle timeout or an upstream
// disconnect and must not affect upstream health scoring.
func MarkLocalStreamMaxDurationExceeded(c *gin.Context) {
	if c != nil {
		c.Set(localStreamMaxDurationContextKey, true)
		MarkLocalStreamTimeout(c, LocalStreamTimeoutMaxDuration)
	}
}

func IsLocalStreamMaxDurationExceeded(c *gin.Context) bool {
	return c != nil && c.GetBool(localStreamMaxDurationContextKey)
}

// MarkLocalStreamTimeout distinguishes gateway-enforced deadlines from an
// upstream EOF so error classification and health accounting remain accurate.
func MarkLocalStreamTimeout(c *gin.Context, reason string) {
	if c != nil && reason != "" {
		c.Set(localStreamTimeoutReasonKey, reason)
	}
}

func LocalStreamTimeoutReason(c *gin.Context) string {
	if c == nil {
		return ""
	}
	return c.GetString(localStreamTimeoutReasonKey)
}
