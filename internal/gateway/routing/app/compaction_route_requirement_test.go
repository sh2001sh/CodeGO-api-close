package app

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	"github.com/stretchr/testify/require"
)

func TestRemoteCompactionCapabilityRank(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set(string(constant.ContextKeyResponsesCompactionRequirement), string(RemoteCompactionRequirementV2))

	supported := &gatewayschema.Channel{ChannelInfo: gatewayschema.ChannelInfo{ResponsesCapabilities: gatewayschema.ResponsesCapabilities{
		RemoteCompactionV2: gatewayschema.CapabilityProbeState{Status: gatewayschema.CapabilityStatusSupported, Model: "gpt-5.6-sol"},
	}}}
	unknown := &gatewayschema.Channel{ChannelInfo: gatewayschema.ChannelInfo{ResponsesCapabilities: gatewayschema.ResponsesCapabilities{
		RemoteCompactionV2: gatewayschema.CapabilityProbeState{Status: gatewayschema.CapabilityStatusError},
	}}}
	unsupported := &gatewayschema.Channel{ChannelInfo: gatewayschema.ChannelInfo{ResponsesCapabilities: gatewayschema.ResponsesCapabilities{
		RemoteCompactionV2: gatewayschema.CapabilityProbeState{Status: gatewayschema.CapabilityStatusUnsupported},
	}}}

	require.Equal(t, 0, remoteCompactionCapabilityRank(ctx, supported, "gpt-5.6-sol"))
	require.Equal(t, 1, remoteCompactionCapabilityRank(ctx, unknown, "gpt-5.6-sol"))
	require.Equal(t, -1, remoteCompactionCapabilityRank(ctx, unsupported, "gpt-5.6-sol"))
	require.Equal(t, -1, remoteCompactionCapabilityRank(ctx, supported, "gpt-5.5"))
}

func TestSetRemoteCompactionRouteRequirementDetectsV2Trigger(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	ctx.Request.Header.Set("X-Codex-Beta-Features", "remote_compaction_v2")
	info := &relaycommon.RelayInfo{
		RelayMode: gatewaycontract.RelayModeResponses,
		Request:   &dto.OpenAIResponsesRequest{Input: json.RawMessage(`[{"type":"message"},{"type":"compaction_trigger"}]`)},
	}

	SetRemoteCompactionRouteRequirement(ctx, info)
	require.Equal(t, RemoteCompactionRequirementV2, remoteCompactionRequirement(ctx))
}

func TestPreferRemoteCompactionCandidatesDropsUnknownWhenSupportedExists(t *testing.T) {
	supported := scoredRoutePoolCandidate{compactionCapabilityRank: 0}
	unknown := scoredRoutePoolCandidate{compactionCapabilityRank: 1}
	healthy, probes, lastResort := preferRemoteCompactionCandidates(
		[]scoredRoutePoolCandidate{unknown},
		[]scoredRoutePoolCandidate{supported},
		nil,
	)

	require.Empty(t, healthy)
	require.Equal(t, []scoredRoutePoolCandidate{supported}, probes)
	require.Empty(t, lastResort)
}
