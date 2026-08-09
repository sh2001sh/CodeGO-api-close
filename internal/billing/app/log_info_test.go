package app

import (
	"net/http/httptest"
	"testing"
	"time"

	gatewaysruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGenerateTextOtherInfoIncludesFirstByteTrace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	startedAt := time.Now().Add(-time.Second)
	relayInfo := &gatewaysruntime.RelayInfo{
		StartTime:         startedAt,
		FirstResponseTime: startedAt.Add(900 * time.Millisecond),
		FirstByteTrace:    gatewaysruntime.NewFirstByteTrace(startedAt),
		StreamPacer:       gatewaysruntime.NewStreamPacer("gpt-5.6-sol"),
		ChannelMeta:       &gatewaysruntime.ChannelMeta{},
	}
	relayInfo.FirstByteTrace.MarkRelayInfoReady()
	relayInfo.FirstByteTrace.MarkPreflightDone()
	relayInfo.FirstByteTrace.MarkRouteSelected()
	relayInfo.FirstByteTrace.MarkUpstreamStart()
	relayInfo.FirstByteTrace.MarkFirstEvent()
	relayInfo.FirstByteTrace.MarkFirstSemanticEvent()

	other := GenerateTextOtherInfo(ctx, relayInfo, 1, 1, 1, 0, 0, 0, 1)

	require.Contains(t, other, "first_byte_trace")
	trace, ok := other["first_byte_trace"].(map[string]int64)
	require.True(t, ok)
	require.Greater(t, trace["total_ms"], int64(0))
}
