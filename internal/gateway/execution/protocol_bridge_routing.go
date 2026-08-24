package execution

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	gatewayproviders "github.com/sh2001sh/new-api/internal/gateway/execution/providers"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	gatewaytranslation "github.com/sh2001sh/new-api/internal/gateway/translation"
	"github.com/sh2001sh/new-api/types"
)

type protocolBridgeDirection uint8

const (
	bridgeChatToResponses protocolBridgeDirection = iota + 1
	bridgeResponsesToChat
	protocolCapabilityTTL = 10 * time.Minute
)

type protocolCapabilityKey struct {
	channelID   int
	channelType int
	direction   protocolBridgeDirection
}

var protocolCapabilityCache sync.Map

func protocolBridgeMode(info *relaycommon.RelayInfo, direction protocolBridgeDirection) gatewaystore.ProtocolBridgeMode {
	settings := gatewaystore.GetGlobalSettings()
	policy := settings.ChatCompletionsToResponsesPolicy
	if direction == bridgeResponsesToChat {
		policy = settings.ResponsesToChatCompletionsPolicy
	}
	return gatewaytranslation.ResolveProtocolBridgeMode(
		policy, info.ChannelId, info.ChannelType, info.OriginModelName,
	)
}

func shouldBridgeBeforeNative(info *relaycommon.RelayInfo, direction protocolBridgeDirection) bool {
	mode := protocolBridgeMode(info, direction)
	if mode == gatewaystore.ProtocolBridgeModeForce {
		return true
	}
	if mode == gatewaystore.ProtocolBridgeModeDisabled {
		return false
	}
	if hasCachedProtocolFallback(info, direction) {
		return true
	}
	return direction == bridgeChatToResponses &&
		(info.ChannelType == constant.ChannelTypeCodex ||
			gatewaycontract.IsOpenAIResponseOnlyModel(info.OriginModelName) ||
			gatewaycontract.IsOpenAIResponseOnlyModel(info.UpstreamModelName))
}

func shouldFallbackAfterConversion(info *relaycommon.RelayInfo, direction protocolBridgeDirection, err error) bool {
	if protocolBridgeMode(info, direction) != gatewaystore.ProtocolBridgeModeAuto || err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "not implemented") ||
		strings.Contains(message, "endpoint not supported") ||
		strings.Contains(message, "not available")
}

func shouldFallbackAfterStatus(info *relaycommon.RelayInfo, direction protocolBridgeDirection, upstreamError *types.NewAPIError) bool {
	if protocolBridgeMode(info, direction) != gatewaystore.ProtocolBridgeModeAuto {
		return false
	}
	if upstreamError == nil {
		return false
	}
	if upstreamError.StatusCode == http.StatusMethodNotAllowed || upstreamError.StatusCode == http.StatusNotImplemented {
		return true
	}
	if upstreamError.StatusCode != http.StatusNotFound {
		return false
	}
	return !strings.Contains(strings.ToLower(upstreamError.Error()), "model")
}

func rememberProtocolFallback(info *relaycommon.RelayInfo, direction protocolBridgeDirection) {
	protocolCapabilityCache.Store(protocolCapabilityCacheKey(info, direction), time.Now().Add(protocolCapabilityTTL))
}

func hasCachedProtocolFallback(info *relaycommon.RelayInfo, direction protocolBridgeDirection) bool {
	key := protocolCapabilityCacheKey(info, direction)
	value, ok := protocolCapabilityCache.Load(key)
	if !ok {
		return false
	}
	expiresAt, ok := value.(time.Time)
	if !ok || time.Now().After(expiresAt) {
		protocolCapabilityCache.Delete(key)
		return false
	}
	return true
}

func protocolCapabilityCacheKey(info *relaycommon.RelayInfo, direction protocolBridgeDirection) protocolCapabilityKey {
	return protocolCapabilityKey{channelID: info.ChannelId, channelType: info.ChannelType, direction: direction}
}

func executeChatToResponsesBridge(c *gin.Context, info *relaycommon.RelayInfo, adaptor gatewayproviders.SyncAdaptor, request *dto.GeneralOpenAIRequest) *types.NewAPIError {
	applySystemPromptIfNeeded(c, info, request)
	usage, bridgeError := chatCompletionsViaResponses(c, info, adaptor, request)
	if bridgeError != nil {
		return bridgeError
	}
	billChatBridgeUsage(c, info, usage)
	return nil
}

func billChatBridgeUsage(c *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage) {
	containsAudioTokens := usage.CompletionTokenDetails.AudioTokens > 0 || usage.PromptTokensDetails.AudioTokens > 0
	containsAudioRatios := gatewaystore.ContainsAudioRatio(info.OriginModelName) || gatewaystore.ContainsAudioCompletionRatio(info.OriginModelName)
	if containsAudioTokens && containsAudioRatios {
		billingapp.PostAudioConsumeQuota(c, info, usage, "")
		return
	}
	billingapp.PostTextConsumeQuota(c, info, usage, nil)
}

func executeResponsesToChatBridge(c *gin.Context, info *relaycommon.RelayInfo, adaptor gatewayproviders.SyncAdaptor, request *dto.OpenAIResponsesRequest) *types.NewAPIError {
	usage, bridgeError := responsesViaChatCompletions(c, info, adaptor, request)
	if bridgeError != nil {
		return bridgeError
	}
	billingapp.PostTextConsumeQuota(c, info, usage, nil)
	return nil
}

func preferBridgeError(originalError, bridgeError *types.NewAPIError) *types.NewAPIError {
	if bridgeError == nil {
		return nil
	}
	var formatError *responsesBridgeFormatError
	if errors.As(bridgeError, &formatError) {
		return originalError
	}
	return bridgeError
}
