package execution

import (
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/constant"
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
