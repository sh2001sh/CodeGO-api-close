package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	gatewayexecutionapp "github.com/sh2001sh/new-api/internal/gateway/execution/app"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	requestsettings "github.com/sh2001sh/new-api/internal/platform/requestsettings"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestRetryRechecksSensitivePromptAfterCountingTextIsReleased(t *testing.T) {
	originalCheck, originalPrompt := requestsettings.CheckSensitiveEnabled, requestsettings.CheckSensitiveOnPromptEnabled
	originalStop, originalWords := requestsettings.StopOnSensitiveEnabled, requestsettings.SensitiveWords
	t.Cleanup(func() {
		requestsettings.CheckSensitiveEnabled, requestsettings.CheckSensitiveOnPromptEnabled = originalCheck, originalPrompt
		requestsettings.StopOnSensitiveEnabled, requestsettings.SensitiveWords = originalStop, originalWords
	})
	requestsettings.CheckSensitiveEnabled, requestsettings.CheckSensitiveOnPromptEnabled = true, true
	requestsettings.StopOnSensitiveEnabled = true
	requestsettings.SensitiveWords = []string{"contains:blocked phrase"}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	request := &dto.GeneralOpenAIRequest{Messages: []dto.Message{{Role: "user", Content: "a blocked phrase"}}}
	meta := request.GetTokenCountMeta()
	meta.MaxTokens = 1484
	intercept := false
	first := &gatewayschema.Channel{SensitiveWordInterceptionEnabled: &intercept}
	require.Nil(t, checkPromptSensitiveRequestForChannel(ctx, types.RelayFormatOpenAI, first, request, meta, false))
	require.Empty(t, meta.CombineText)
	intercept = true
	blocked := checkPromptSensitiveRequestForChannel(ctx, types.RelayFormatOpenAI, first, request, meta, true)
	require.NotNil(t, blocked, "retry must rehydrate text when the next channel enables interception")
	require.Equal(t, http.StatusForbidden, blocked.StatusCode)
	require.Empty(t, meta.CombineText)
	require.Equal(t, 1484, meta.MaxTokens, "pricing scalars must survive prompt release")
}

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
	blocked := checkPromptSensitiveForChannel(ctx, types.RelayFormatOpenAI, enabled, meta)
	require.NotNil(t, blocked)
	require.Equal(t, http.StatusForbidden, blocked.StatusCode)
	require.Equal(t, types.ErrorCodeSensitiveWordsDetected, blocked.GetErrorCode())
	require.True(t, types.IsSkipRetryError(blocked))
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

func TestFirstRelayAttemptPreservesOfficialChannelSensitiveWordPolicy(t *testing.T) {
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
	disabled := false
	channel := &gatewayschema.Channel{
		Id: 6, Type: constant.ChannelTypeOpenAI, Key: "channel-key",
		Name: "official-six", ChannelScope: gatewayschema.ChannelScopeOfficial,
		SensitiveWordInterceptionEnabled: &disabled,
	}
	require.Nil(t, gatewayexecutionapp.SetupContextForSelectedChannel(ctx, channel, "gpt-test"))

	selected := selectedDistributorChannel(ctx)
	require.Equal(t, channel.Id, selected.Id)
	require.NotNil(t, selected.SensitiveWordInterceptionEnabled)
	require.False(t, *selected.SensitiveWordInterceptionEnabled)
	meta := &types.TokenCountMeta{CombineText: "contains a blocked phrase"}
	require.Nil(t, checkPromptSensitiveForChannel(ctx, types.RelayFormatOpenAI, selected, meta))
}

func TestFirstRelayAttemptDefaultsSensitiveWordPolicyToEnabled(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	selected := selectedDistributorChannel(ctx)

	require.NotNil(t, selected.SensitiveWordInterceptionEnabled)
	require.True(t, *selected.SensitiveWordInterceptionEnabled)
}

func TestClaudeRequestsSkipPromptSensitiveInterception(t *testing.T) {
	require.False(t, shouldCheckPromptSensitiveForRelay(types.RelayFormatClaude))
	require.True(t, shouldCheckPromptSensitiveForRelay(types.RelayFormatOpenAI))
}
