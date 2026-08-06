package runtime

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
)

// StreamMaxDuration returns the absolute upstream stream budget. GPT requests
// use a shorter normal budget and a longer budget for large prompts; other
// protocols retain their existing idle-time behavior until they gain a
// protocol-specific total-duration policy.
func StreamMaxDuration(model string, promptTokens int) time.Duration {
	if !isGPTModel(model) {
		return 0
	}
	if promptTokens >= LongContextPromptTokenThreshold {
		if constant.StreamingLongContextMaxDuration > 0 {
			return time.Duration(constant.StreamingLongContextMaxDuration) * time.Second
		}
		return 540 * time.Second
	}
	if constant.StreamingMaxDuration > 0 {
		return time.Duration(constant.StreamingMaxDuration) * time.Second
	}
	return 240 * time.Second
}

// StreamMaxDurationForRequest applies the same tiering as StreamMaxDuration,
// including an affinity-scoped upstream usage high-water mark for Responses
// requests that only carry a small delta locally.
func StreamMaxDurationForRequest(c *gin.Context, model string, promptTokens int) time.Duration {
	if c != nil && IsLongContextRequest(c) && isGPTModel(model) {
		return StreamMaxDuration(model, LongContextPromptTokenThreshold)
	}
	return StreamMaxDuration(model, promptTokens)
}

const (
	LongContextPromptTokenThreshold = 100_000
	VeryLongContextPromptTokens     = 200_000

	longContextRequestContextKey = "long_context_gpt_request"
)

// IsLongContextGPTRequest reports whether a GPT request needs the long-context
// reliability policy before its upstream response begins streaming.
func IsLongContextGPTRequest(model string, promptTokens int) bool {
	return promptTokens >= LongContextPromptTokenThreshold && isGPTModel(model)
}

// MarkLongContextRequest records the request classification for route retries
// and channel health handling. It stores no request content.
func MarkLongContextRequest(c *gin.Context, model string, promptTokens int) {
	if c == nil {
		return
	}
	if observed := ConversationPromptHighWaterFromContext(c, model); observed > promptTokens {
		promptTokens = observed
	}
	c.Set(longContextRequestContextKey, IsLongContextGPTRequest(model, promptTokens))
}

func IsLongContextRequest(c *gin.Context) bool {
	return c != nil && c.GetBool(longContextRequestContextKey)
}

func isGPTModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "gpt-")
}
