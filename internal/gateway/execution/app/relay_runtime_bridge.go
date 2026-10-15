package app

import (
	"errors"

	"github.com/gin-gonic/gin"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	"github.com/sh2001sh/new-api/types"
)

type relayRuntimeFunc func(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError

type RelayRuntime struct {
	Text            relayRuntimeFunc
	Image           relayRuntimeFunc
	Audio           relayRuntimeFunc
	Rerank          relayRuntimeFunc
	Embedding       relayRuntimeFunc
	Responses       relayRuntimeFunc
	AlphaSearch     relayRuntimeFunc
	Gemini          relayRuntimeFunc
	GeminiEmbedding relayRuntimeFunc
	Claude          relayRuntimeFunc
	Realtime        relayRuntimeFunc
}

var gatewayRelayRuntime RelayRuntime

func SetRelayRuntime(runtime RelayRuntime) {
	gatewayRelayRuntime = runtime
}

func ExecuteRelay(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	switch info.RelayMode {
	case gatewaycontract.RelayModeImagesGenerations, gatewaycontract.RelayModeImagesEdits:
		return callRelayRuntime(gatewayRelayRuntime.Image, "image", c, info)
	case gatewaycontract.RelayModeAudioSpeech, gatewaycontract.RelayModeAudioTranslation, gatewaycontract.RelayModeAudioTranscription:
		return callRelayRuntime(gatewayRelayRuntime.Audio, "audio", c, info)
	case gatewaycontract.RelayModeRerank:
		return callRelayRuntime(gatewayRelayRuntime.Rerank, "rerank", c, info)
	case gatewaycontract.RelayModeEmbeddings:
		return callRelayRuntime(gatewayRelayRuntime.Embedding, "embedding", c, info)
	case gatewaycontract.RelayModeResponses, gatewaycontract.RelayModeResponsesCompact:
		return callRelayRuntime(gatewayRelayRuntime.Responses, "responses", c, info)
	case gatewaycontract.RelayModeAlphaSearch:
		return callRelayRuntime(gatewayRelayRuntime.AlphaSearch, "alpha_search", c, info)
	default:
		return callRelayRuntime(gatewayRelayRuntime.Text, "text", c, info)
	}
}

func ExecuteGeminiRelay(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	return callRelayRuntime(gatewayRelayRuntime.Gemini, "gemini", c, info)
}

func ExecuteGeminiEmbeddingRelay(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	return callRelayRuntime(gatewayRelayRuntime.GeminiEmbedding, "gemini_embedding", c, info)
}

func ExecuteClaudeRelay(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	return callRelayRuntime(gatewayRelayRuntime.Claude, "claude", c, info)
}

func ExecuteRealtimeRelay(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	return callRelayRuntime(gatewayRelayRuntime.Realtime, "realtime", c, info)
}

func callRelayRuntime(fn relayRuntimeFunc, name string, c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	if fn != nil {
		return fn(c, info)
	}
	return types.NewError(errors.New(name+"_relay_runtime_not_initialized"), types.ErrorCodeDoRequestFailed)
}
