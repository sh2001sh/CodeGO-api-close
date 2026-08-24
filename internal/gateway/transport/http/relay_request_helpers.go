package http

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	identityapp "github.com/sh2001sh/new-api/internal/identity/app"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	requestsettings "github.com/sh2001sh/new-api/internal/platform/requestsettings"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/sh2001sh/new-api/types"
)

func channelConcurrencyRejection(
	admission relaycommon.ChannelConcurrencyAdmission,
) (string, *types.NewAPIError, bool) {
	if admission == relaycommon.ChannelConcurrencyCapacityReached {
		return "channel_capacity", types.NewErrorWithStatusCode(
			errors.New("channel concurrency limit reached"),
			types.ErrorCodeGetChannelFailed,
			http.StatusServiceUnavailable,
		), true
	}
	// Redis admission failed before any upstream attempt. Keep it distinct
	// from channel capacity and do not feed it into health or cooldowns.
	return "channel_concurrency_dependency", types.NewErrorWithStatusCode(
		errors.New("channel concurrency service unavailable"),
		types.ErrorCodeServiceBusy,
		http.StatusServiceUnavailable,
		types.ErrOptionWithSkipRetry(),
	), false
}

func traceFromContext(c *gin.Context) *relaycommon.FirstByteTrace {
	if value, exists := c.Get(relaycommon.FirstByteTraceContextKey); exists {
		if trace, ok := value.(*relaycommon.FirstByteTrace); ok && trace != nil {
			return trace
		}
	}
	trace := relaycommon.NewFirstByteTrace(httpctx.GetContextKeyTime(c, constant.ContextKeyRequestStartTime))
	c.Set(relaycommon.FirstByteTraceContextKey, trace)
	c.Set(platformhttpx.KeyBodyTiming, trace)
	return trace
}

func specialMultiplierUnsupportedRelay(relayFormat types.RelayFormat, modelName string) bool {
	if gatewaycontract.IsImageGenerationModel(modelName) {
		return true
	}
	switch relayFormat {
	case types.RelayFormatOpenAIImage, types.RelayFormatOpenAIAudio, types.RelayFormatOpenAIRealtime:
		return true
	default:
		return false
	}
}

func shouldBlockSensitiveWords() bool {
	return requestsettings.StopOnSensitiveEnabled
}

func shouldCheckPromptSensitiveForRelay(relayFormat types.RelayFormat) bool {
	// Claude requests commonly include provider safety/system/tool text. Do
	// not apply the site's prompt interception to that protocol, otherwise
	// harmless user prompts can match vocabulary embedded in metadata.
	return requestsettings.ShouldCheckPromptSensitive() && relayFormat != types.RelayFormatClaude
}

func checkPromptSensitiveForChannel(
	c *gin.Context,
	relayFormat types.RelayFormat,
	channel *gatewayschema.Channel,
	meta *types.TokenCountMeta,
) *types.NewAPIError {
	if !shouldCheckPromptSensitiveForRelay(relayFormat) || meta == nil {
		return nil
	}
	if !channel.ShouldInterceptSensitiveWords() {
		return nil
	}
	contains, words := identityapp.CheckSensitiveText(meta.CombineText)
	if !contains {
		return nil
	}
	logger.LogWarn(c, fmt.Sprintf("user sensitive words detected: %s", strings.Join(words, ", ")))
	if shouldBlockSensitiveWords() {
		return types.NewError(errors.New("sensitive words detected"), types.ErrorCodeSensitiveWordsDetected)
	}
	return nil
}
