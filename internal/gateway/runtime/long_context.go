package runtime

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
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

// StreamFirstOutputTimeoutForRequest returns the bounded wait for a GPT
// stream to become useful. Long-context continuations receive a larger
// bootstrap budget because the upstream may restore hidden history first.
func StreamFirstOutputTimeoutForRequest(c *gin.Context, model string, promptTokens int) time.Duration {
	if !isGPTModel(model) {
		return 0
	}
	if (c != nil && IsLongContextRequest(c)) || promptTokens >= LongContextPromptTokenThreshold {
		if constant.StreamingLongContextFirstByteTimeout > 0 {
			return time.Duration(constant.StreamingLongContextFirstByteTimeout) * time.Second
		}
		return 90 * time.Second
	}
	if constant.StreamingFirstByteTimeout > 0 {
		return time.Duration(constant.StreamingFirstByteTimeout) * time.Second
	}
	return 45 * time.Second
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
	MarkLongContextRequestWithContinuation(c, model, promptTokens, false)
}

// MarkLongContextRequestWithContinuation classifies a GPT Responses session
// as long-context before a successful upstream usage sample is available. A
// prompt_cache_key is itself a stable upstream conversation/session signal;
// previous_response_id is optional in Codex fork/continue flows.
func MarkLongContextRequestWithContinuation(c *gin.Context, model string, promptTokens int, hasConversationState bool) {
	if c == nil {
		return
	}
	if observed := ConversationPromptHighWaterFromContext(c, model); observed > promptTokens {
		promptTokens = observed
	}
	c.Set(longContextRequestContextKey, hasConversationState && isGPTModel(model) || IsLongContextGPTRequest(model, promptTokens))
}

// IsResponsesConversationRequest reports whether a parsed Responses request
// carries a stable conversation/session signal. The raw key value is never
// retained; only its presence is used for timeout classification.
func IsResponsesConversationRequest(request dto.Request) bool {
	switch request := request.(type) {
	case *dto.OpenAIResponsesRequest:
		return strings.TrimSpace(request.PreviousResponseID) != "" || hasJSONValue(request.PromptCacheKey)
	case *dto.OpenAIResponsesCompactionRequest:
		return strings.TrimSpace(request.PreviousResponseID) != ""
	default:
		return false
	}
}

func hasJSONValue(value []byte) bool {
	trimmed := strings.TrimSpace(string(value))
	return trimmed != "" && trimmed != "null" && trimmed != `""`
}

func IsLongContextRequest(c *gin.Context) bool {
	return c != nil && c.GetBool(longContextRequestContextKey)
}

func isGPTModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "gpt-")
}
