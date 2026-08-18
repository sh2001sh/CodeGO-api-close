package http

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	requestsettings "github.com/sh2001sh/new-api/internal/platform/requestsettings"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestShouldBlockSensitiveWordsHonorsStopOnSensitiveEnabled(t *testing.T) {
	original := requestsettings.StopOnSensitiveEnabled
	t.Cleanup(func() { requestsettings.StopOnSensitiveEnabled = original })

	requestsettings.StopOnSensitiveEnabled = false
	require.False(t, shouldBlockSensitiveWords())

	requestsettings.StopOnSensitiveEnabled = true
	require.True(t, shouldBlockSensitiveWords())
}

func TestExternalChannelControlsPromptSensitiveInterception(t *testing.T) {
	originalCheck := requestsettings.CheckSensitiveEnabled
	originalPrompt := requestsettings.CheckSensitiveOnPromptEnabled
	originalStop := requestsettings.StopOnSensitiveEnabled
	originalWords := requestsettings.SensitiveWords
	t.Cleanup(func() {
		requestsettings.CheckSensitiveEnabled = originalCheck
		requestsettings.CheckSensitiveOnPromptEnabled = originalPrompt
		requestsettings.StopOnSensitiveEnabled = originalStop
		requestsettings.SensitiveWords = originalWords
	})
	requestsettings.CheckSensitiveEnabled = true
	requestsettings.CheckSensitiveOnPromptEnabled = true
	requestsettings.StopOnSensitiveEnabled = true
	requestsettings.SensitiveWords = []string{"contains:blocked phrase"}

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	meta := &types.TokenCountMeta{CombineText: "contains a blocked phrase"}
	disabledValue := false
	enabledValue := true
	disabled := &gatewayschema.Channel{ChannelScope: gatewayschema.ChannelScopeExternal, SensitiveWordInterceptionEnabled: &disabledValue}
	enabled := &gatewayschema.Channel{ChannelScope: gatewayschema.ChannelScopeExternal, SensitiveWordInterceptionEnabled: &enabledValue}
	official := &gatewayschema.Channel{ChannelScope: gatewayschema.ChannelScopeOfficial, SensitiveWordInterceptionEnabled: &disabledValue}

	require.Nil(t, checkPromptSensitiveForChannel(ctx, types.RelayFormatOpenAI, disabled, meta))
	require.NotNil(t, checkPromptSensitiveForChannel(ctx, types.RelayFormatOpenAI, enabled, meta))
	require.Nil(t, checkPromptSensitiveForChannel(ctx, types.RelayFormatOpenAI, official, meta))
}

func TestOfficialChannelSensitiveInterceptionDefaultsToEnabled(t *testing.T) {
	originalCheck := requestsettings.CheckSensitiveEnabled
	originalPrompt := requestsettings.CheckSensitiveOnPromptEnabled
	originalStop := requestsettings.StopOnSensitiveEnabled
	originalWords := requestsettings.SensitiveWords
	t.Cleanup(func() {
		requestsettings.CheckSensitiveEnabled = originalCheck
		requestsettings.CheckSensitiveOnPromptEnabled = originalPrompt
		requestsettings.StopOnSensitiveEnabled = originalStop
		requestsettings.SensitiveWords = originalWords
	})
	requestsettings.CheckSensitiveEnabled = true
	requestsettings.CheckSensitiveOnPromptEnabled = true
	requestsettings.StopOnSensitiveEnabled = true
	requestsettings.SensitiveWords = []string{"contains:blocked phrase"}

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	meta := &types.TokenCountMeta{CombineText: "contains a blocked phrase"}
	official := &gatewayschema.Channel{ChannelScope: gatewayschema.ChannelScopeOfficial}

	require.NotNil(t, checkPromptSensitiveForChannel(ctx, types.RelayFormatOpenAI, official, meta))
}

func TestClaudeRequestsSkipPromptSensitiveInterception(t *testing.T) {
	require.False(t, shouldCheckPromptSensitiveForRelay(types.RelayFormatClaude))
	require.True(t, shouldCheckPromptSensitiveForRelay(types.RelayFormatOpenAI))
}
