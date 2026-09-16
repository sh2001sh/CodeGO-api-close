package execution

import (
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestAutomaticBridgeRecognizesResponsesOnlyTargets(t *testing.T) {
	settings := gatewaystore.GetGlobalSettings()
	original := settings.ChatCompletionsToResponsesPolicy
	t.Cleanup(func() { settings.ChatCompletionsToResponsesPolicy = original })
	settings.ChatCompletionsToResponsesPolicy = gatewaystore.ProtocolBridgePolicy{Mode: gatewaystore.ProtocolBridgeModeAuto}

	require.True(t, shouldBridgeBeforeNative(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeCodex},
	}, bridgeChatToResponses))
	require.True(t, shouldBridgeBeforeNative(&relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{},
		OriginModelName: "o3-pro",
	}, bridgeChatToResponses))
	require.False(t, shouldBridgeBeforeNative(&relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{},
		OriginModelName: "gpt-4o-mini",
	}, bridgeChatToResponses))
}

func TestProtocolBridgeOverrideCanForceOrDisableAutomaticRouting(t *testing.T) {
	settings := gatewaystore.GetGlobalSettings()
	original := settings.ChatCompletionsToResponsesPolicy
	t.Cleanup(func() { settings.ChatCompletionsToResponsesPolicy = original })
	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 42, ChannelType: constant.ChannelTypeCodex},
		OriginModelName: "gpt-5",
	}

	settings.ChatCompletionsToResponsesPolicy = gatewaystore.ProtocolBridgePolicy{
		Mode: gatewaystore.ProtocolBridgeModeDisabled, AllChannels: true,
	}
	require.False(t, shouldBridgeBeforeNative(info, bridgeChatToResponses))

	settings.ChatCompletionsToResponsesPolicy = gatewaystore.ProtocolBridgePolicy{
		Mode: gatewaystore.ProtocolBridgeModeForce, ChannelIDs: []int{42},
	}
	require.True(t, shouldBridgeBeforeNative(info, bridgeChatToResponses))
}

func TestAutomaticFallbackRecognizesUnsupportedConversionAndEndpoint(t *testing.T) {
	settings := gatewaystore.GetGlobalSettings()
	original := settings.ResponsesToChatCompletionsPolicy
	t.Cleanup(func() { settings.ResponsesToChatCompletionsPolicy = original })
	settings.ResponsesToChatCompletionsPolicy = gatewaystore.ProtocolBridgePolicy{Mode: gatewaystore.ProtocolBridgeModeAuto}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 8, ChannelType: constant.ChannelTypeDeepSeek},
	}

	require.True(t, shouldFallbackAfterConversion(info, bridgeResponsesToChat, errors.New("not implemented")))
	require.True(t, shouldFallbackAfterStatus(info, bridgeResponsesToChat,
		types.NewOpenAIError(errors.New("route not found"), types.ErrorCodeBadResponseStatusCode, http.StatusNotFound)))
	require.False(t, shouldFallbackAfterStatus(info, bridgeResponsesToChat,
		types.NewOpenAIError(errors.New("model not found"), types.ErrorCodeModelNotFound, http.StatusNotFound)))
	require.False(t, shouldFallbackAfterStatus(info, bridgeResponsesToChat,
		types.NewOpenAIError(errors.New("bad request"), types.ErrorCodeBadResponseStatusCode, http.StatusBadRequest)))
}

func TestReasoningToolsUseResponsesForGPT54AndNewer(t *testing.T) {
	settings := gatewaystore.GetGlobalSettings()
	original := settings.ChatCompletionsToResponsesPolicy
	t.Cleanup(func() { settings.ChatCompletionsToResponsesPolicy = original })
	settings.ChatCompletionsToResponsesPolicy = gatewaystore.ProtocolBridgePolicy{Mode: gatewaystore.ProtocolBridgeModeAuto}

	request := &dto.GeneralOpenAIRequest{
		ReasoningEffort: "medium",
		Tools:           []dto.ToolCallRequest{{Type: "function", Function: dto.FunctionRequest{Name: "lookup"}}},
	}
	require.True(t, shouldBridgeChatReasoningTools(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, OriginModelName: "gpt-5.6-sol"}, request))
	require.True(t, shouldBridgeChatReasoningTools(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-6-astra"}}, request))
	require.False(t, shouldBridgeChatReasoningTools(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, OriginModelName: "gpt-5.3"}, request))

	request.ReasoningEffort = "none"
	require.False(t, shouldBridgeChatReasoningTools(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, OriginModelName: "gpt-5.6-sol"}, request))
}

func TestAutomaticFallbackRecognizesReasoningToolsChatRejection(t *testing.T) {
	settings := gatewaystore.GetGlobalSettings()
	original := settings.ChatCompletionsToResponsesPolicy
	t.Cleanup(func() { settings.ChatCompletionsToResponsesPolicy = original })
	settings.ChatCompletionsToResponsesPolicy = gatewaystore.ProtocolBridgePolicy{Mode: gatewaystore.ProtocolBridgeModeAuto}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 437, ChannelType: constant.ChannelTypeOpenAI}}

	err := types.NewOpenAIError(errors.New("Function tools with reasoning_effort are not supported for gpt-5.6-sol in /v1/chat/completions. To use function tools, use /v1/responses or set reasoning_effort to 'none'."), types.ErrorCodeBadResponseStatusCode, http.StatusBadRequest)
	require.True(t, shouldFallbackAfterStatus(info, bridgeChatToResponses, err))
	require.False(t, shouldFallbackAfterStatus(info, bridgeResponsesToChat, err))
}

func TestChatRequestNeedsRewriteForOrphanParallelToolCalls(t *testing.T) {
	parallel := true
	require.True(t, chatRequestNeedsOpenAIRewrite("gpt-4o", &dto.GeneralOpenAIRequest{ParallelTooCalls: &parallel}))
	require.False(t, chatRequestNeedsOpenAIRewrite("gpt-4o", &dto.GeneralOpenAIRequest{
		ParallelTooCalls: &parallel,
		Tools:            []dto.ToolCallRequest{{Type: "function", Function: dto.FunctionRequest{Name: "lookup"}}},
	}))
}

func TestSuccessfulFallbackCapabilityIsCached(t *testing.T) {
	protocolCapabilityCache = sync.Map{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 19, ChannelType: constant.ChannelTypeOpenRouter},
	}
	rememberProtocolFallback(info, bridgeResponsesToChat)
	require.True(t, hasCachedProtocolFallback(info, bridgeResponsesToChat))

	key := protocolCapabilityCacheKey(info, bridgeResponsesToChat)
	protocolCapabilityCache.Store(key, time.Now().Add(-time.Second))
	require.False(t, hasCachedProtocolFallback(info, bridgeResponsesToChat))
}
