package runtime

import (
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	LongContextPromptTokenThreshold = 100_000
	VeryLongContextPromptTokens     = 200_000

	longContextRequestContextKey = "long_context_gpt_request"
)

// IsLongContextGPTRequest reports whether a GPT request needs the long-context
// reliability policy before its upstream response begins streaming.
func IsLongContextGPTRequest(model string, promptTokens int) bool {
	return promptTokens >= LongContextPromptTokenThreshold &&
		strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "gpt-")
}

// MarkLongContextRequest records the request classification for route retries
// and channel health handling. It stores no request content.
func MarkLongContextRequest(c *gin.Context, model string, promptTokens int) {
	if c == nil {
		return
	}
	c.Set(longContextRequestContextKey, IsLongContextGPTRequest(model, promptTokens))
}

func IsLongContextRequest(c *gin.Context) bool {
	return c != nil && c.GetBool(longContextRequestContextKey)
}
