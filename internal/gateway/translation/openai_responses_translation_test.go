package translation

import (
	"testing"

	"github.com/sh2001sh/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestResponsesResponseToChatPreservesCacheWriteTokens(t *testing.T) {
	response := &dto.OpenAIResponsesResponse{
		Usage: &dto.Usage{
			InputTokens: 120, OutputTokens: 30, TotalTokens: 150,
			InputTokensDetails: &dto.InputTokenDetails{
				CachedTokens:         80,
				CachedCreationTokens: 20,
				CacheWriteTokensDetails: &dto.CacheWriteTokensDetails{
					TTL30mTokens: 12,
					TTL12hTokens: 8,
				},
			},
		},
	}

	chat, usage, err := ResponsesResponseToChatCompletionsResponse(response, "chat-cache-write")
	require.NoError(t, err)
	require.Equal(t, 20, usage.PromptTokensDetails.CachedCreationTokens)
	require.Equal(t, 20, chat.Usage.PromptTokensDetails.CachedCreationTokens)
	require.Equal(t, 12, usage.PromptTokensDetails.CacheWriteTokensDetails.TTL30mTokens)
	require.Equal(t, 8, chat.Usage.PromptTokensDetails.CacheWriteTokensDetails.TTL12hTokens)
}
