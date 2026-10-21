package stream

import (
	"context"
	"errors"
	"io"
	"net/http"
	"syscall"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	"github.com/sh2001sh/new-api/internal/platform/tokenx"
	"github.com/sh2001sh/new-api/types"
)

func isDownstreamDisconnect(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET)
}

// RecoveredUsage is called after ScanResponse has joined its workers. Exact
// provider usage wins; estimates are explicitly marked for reconciliation.
func RecoveredUsage(c *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage) (*dto.Usage, *types.NewAPIError) {
	if info == nil || info.StreamUsageRecovery == nil {
		return usage, nil
	}
	provider, text, metadata := info.StreamUsageRecovery.Snapshot()
	c.Set("stream_usage_recovery", metadata)
	if failure := info.StreamUsageRecovery.Failure(); failure != nil {
		c.Set(string(constant.ContextKeyUpstreamTerminalError), true)
		return nil, types.WithOpenAIError(*failure, http.StatusBadGateway)
	}
	attempted, _ := metadata["attempted"].(bool)
	if !attempted && !IsClientGone(c) {
		return usage, nil
	}
	MarkClientGone(c)
	if provider != nil {
		provider.TotalTokens = provider.PromptTokens + provider.CompletionTokens
		provider.UsageSource = "upstream_final_usage_recovered"
		return provider, nil
	}
	c.Set("usage_reconciliation_required", true)
	if usage == nil {
		usage = &dto.Usage{}
	}
	if usage.PromptTokens == 0 {
		usage.PromptTokens = info.GetEstimatePromptTokens()
	}
	if text != "" {
		if tokens := tokenx.EstimateTokenByModel(info.UpstreamModelName, text); tokens > usage.CompletionTokens {
			usage.CompletionTokens = tokens
		}
	}
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	usage.UsageSource = "local_incomplete_usage"
	return usage, nil
}
