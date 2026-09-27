package dto

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeminiChatRequestTokenMetaIncludesConvertedPromptFields(t *testing.T) {
	request := GeminiChatRequest{
		SystemInstructions: &GeminiChatContent{
			Parts: []GeminiPart{{Text: "system policy that must be billed"}},
		},
		Contents: []GeminiChatContent{
			{
				Role: "user",
				Parts: []GeminiPart{
					{Text: "visible prompt"},
					{FunctionCall: &FunctionCall{FunctionName: "lookup_account", Arguments: map[string]any{"account_id": "acct-123"}}},
				},
			},
			{
				Role: "function",
				Parts: []GeminiPart{{FunctionResponse: &GeminiFunctionResponse{
					Name:     "lookup_account",
					Response: map[string]any{"status": "active"},
				}}},
			},
		},
		Tools: []byte(`[{"functionDeclarations":[{"name":"lookup_account","description":"find an account","parameters":{"type":"object","properties":{"account_id":{"type":"string"}}}}]}]`),
	}

	meta := request.GetTokenCountMeta()

	require.NotNil(t, meta)
	for _, expected := range []string{
		"system policy that must be billed",
		"visible prompt",
		"lookup_account",
		"acct-123",
		"active",
		"find an account",
		"account_id",
	} {
		require.Truef(t, strings.Contains(meta.CombineText, expected), "token input must include %q", expected)
	}
	require.Equal(t, 3, meta.MessagesCount)
	require.Equal(t, 1, meta.ToolsCount)
}
