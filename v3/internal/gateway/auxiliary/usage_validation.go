package auxiliary

import (
	"math"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func validUsage(u gateway.Usage) bool {
	for _, count := range []int64{u.PromptTokens, u.CompletionTokens, u.CachedTokens,
		u.CacheWriteTokens, u.CacheWrite1hTokens, u.ImageInputTokens, u.ImageOutputTokens,
		u.AudioInputTokens, u.AudioOutputTokens, u.ImageCount, u.AudioCharacters,
		u.AudioDurationMicros, u.VideoDurationMicros} {
		if count < 0 {
			return false
		}
	}
	for _, count := range u.ToolCalls {
		if count < 0 {
			return false
		}
	}
	return u.CacheWriteTokens <= math.MaxInt64-u.CacheWrite1hTokens
}
