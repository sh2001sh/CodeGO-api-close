package runtime

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestInvalidateChannelAffinityReenablesRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(nil)
	setChannelAffinityContext(context, channelAffinityMeta{
		CacheKey:   "retry-after-affinity-failure",
		TTLSeconds: 60,
		SkipRetry:  true,
	})
	context.Set(ginKeyChannelAffinitySkipRetry, true)

	InvalidateChannelAffinityForCurrentRequest(context)

	require.False(t, ShouldSkipRetryAfterChannelAffinityFailure(context))
}
