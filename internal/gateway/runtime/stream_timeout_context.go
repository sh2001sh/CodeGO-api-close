package runtime

import "github.com/gin-gonic/gin"

const localStreamMaxDurationContextKey = "local_stream_max_duration_exceeded"

// MarkLocalStreamMaxDurationExceeded identifies a gateway-enforced total
// stream duration expiry. It is distinct from an idle timeout or an upstream
// disconnect and must not affect upstream health scoring.
func MarkLocalStreamMaxDurationExceeded(c *gin.Context) {
	if c != nil {
		c.Set(localStreamMaxDurationContextKey, true)
	}
}

func IsLocalStreamMaxDurationExceeded(c *gin.Context) bool {
	return c != nil && c.GetBool(localStreamMaxDurationContextKey)
}
