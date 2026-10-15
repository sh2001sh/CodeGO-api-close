package securityaudit

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseQwen3GuardFlagsControversialJailbreak(t *testing.T) {
	result, err := ParseQwen3Guard("Safety: Controversial\nCategories: Jailbreak", allScannerIDs)
	require.NoError(t, err)
	require.Equal(t, "warn", result.Action)
	require.Equal(t, []string{"jailbreak"}, result.MatchedScanners)
}

func TestParseQwen3GuardAllowsSafePrompt(t *testing.T) {
	result, err := ParseQwen3Guard("Safety: Safe\nCategories: None", allScannerIDs)
	require.NoError(t, err)
	require.Equal(t, "allow", result.Action)
}

func TestParseQwen3GuardRejectsInvalidContract(t *testing.T) {
	invalid := []string{
		"This prompt seems safe",
		"Safety: Safe\nCategories: None\nExplanation: accepted",
		"Safety: Safe\nSafety: Safe\nCategories: None",
		"```\nSafety: Safe\nCategories: None\n```",
	}
	for _, content := range invalid {
		_, err := ParseQwen3Guard(content, allScannerIDs)
		require.Error(t, err)
		require.Equal(t, ErrorCodeInvalidResponse, guardErrorCode(err))
	}
}

func TestChatCompletionsURL(t *testing.T) {
	url, err := chatCompletionsURL("https://guard.example/v1")
	require.NoError(t, err)
	require.Equal(t, "https://guard.example/v1/chat/completions", url)
}
