package app

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	"github.com/stretchr/testify/require"
)

func TestPrioritizeAutoRouteBindingsReadsLiveHealthAndRecordsReason(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	ctx.Set(string(constant.ContextKeyUserId), 924617)
	profile := gatewayruntime.InitializeRequestProfile(ctx, "gpt-live-rank-test", ctx.Request.URL.Path, gatewayruntime.RequestProfileHint{IsStream: true})
	gatewayruntime.StartRouteDecision(ctx, "gpt-live-rank-test", "auto")
	for i := 0; i < 5; i++ {
		gatewayruntime.RecordChannelSuccess(924617, "gpt-live-rank-test", 45*time.Second, profile.RequestType)
		gatewayruntime.RecordChannelSuccess(924618, "gpt-live-rank-test", 2*time.Second, profile.RequestType)
	}
	bindings := []RoutingBinding{{InternalGroup: "slow", InternalChannelID: 924617}, {InternalGroup: "fast", InternalChannelID: 924618}}
	ordered := PrioritizeAutoRouteBindings(ctx, bindings, "gpt-live-rank-test")
	require.Equal(t, "fast", ordered[0].InternalGroup)
	require.Equal(t, "slow", bindings[0].InternalGroup)
	decision, ok := gatewayruntime.GetRouteDecision(ctx)
	require.True(t, ok)
	require.Len(t, decision.LiveGroupSignals, 1)
	require.Equal(t, "slow_recent_ttft", decision.LiveGroupSignals[0].Reason)
}

func TestAutoBindingsPreserveConfiguredOrderWithoutDistress(t *testing.T) {
	bindings := []RoutingBinding{{InternalGroup: "cheap", InternalChannelID: 1}, {InternalGroup: "preferred", InternalChannelID: 2, MaxConcurrency: 100}}
	states := map[int]*autoRouteLiveState{1: {}, 2: {}}
	ordered, _ := prioritizeAutoRouteBindingsSnapshot(bindings, map[int]int{1: 1}, states, time.Now())
	require.Equal(t, bindings, ordered, "unlimited channels are not full at one request")
	ordered[0].InternalGroup = "changed"
	require.Equal(t, "cheap", bindings[0].InternalGroup, "do not mutate cached bindings")
}

func TestAutoBindingsMoveLoadAndSlowHeadersBehindHealthyGroups(t *testing.T) {
	now := time.Now()
	bindings := []RoutingBinding{{InternalGroup: "cheap-large-limit", InternalChannelID: 1, MaxConcurrency: 1000}, {InternalGroup: "full", InternalChannelID: 2, MaxConcurrency: 10}, {InternalGroup: "available", InternalChannelID: 3}}
	states := map[int]*autoRouteLiveState{1: {shared: gatewayruntime.ChannelHealth{TTFTSamples: 10, TTFTP95Milliseconds: 75000, LastSuccessAt: now}}, 2: {}, 3: {shared: gatewayruntime.ChannelHealth{TTFTSamples: 10, TTFTP95Milliseconds: 2000, LastSuccessAt: now}}}
	ordered, reasons := prioritizeAutoRouteBindingsSnapshot(bindings, map[int]int{1: 1, 2: 9, 3: 0}, states, now)
	require.Equal(t, "available", ordered[0].InternalGroup)
	require.Equal(t, "slow_recent_ttft", reasons[1])
	require.Equal(t, "high_inflight", reasons[2])
	states[1].shared.LastSuccessAt = now.Add(-10 * time.Minute)
	ordered, reasons = prioritizeAutoRouteBindingsSnapshot(bindings, map[int]int{1: 1, 2: 0, 3: 0}, states, now)
	require.Equal(t, "cheap-large-limit", ordered[0].InternalGroup, "stale latency cannot pin a route down")
	require.Empty(t, reasons[1])
}

func TestAutoBindingsDetectRelativeLoadDespiteLargeConfiguredLimits(t *testing.T) {
	bindings := []RoutingBinding{{InternalChannelID: 1, MaxConcurrency: 1000}, {InternalChannelID: 2, MaxConcurrency: 1000}}
	states := map[int]*autoRouteLiveState{1: {}, 2: {}}
	ordered, _ := prioritizeAutoRouteBindingsSnapshot(bindings, map[int]int{1: 12, 2: 1}, states, time.Now())
	require.Equal(t, 2, ordered[0].InternalChannelID)
}

func TestAutoBindingsAvoidRecentFailureWithoutDeletingLastResort(t *testing.T) {
	now := time.Now()
	bindings := []RoutingBinding{{InternalChannelID: 1}, {InternalChannelID: 2}, {InternalChannelID: 3}}
	states := map[int]*autoRouteLiveState{
		1: {user: gatewayruntime.ChannelHealth{LastFailureAt: now, CoolingUntil: now.Add(time.Minute)}},
		2: {shared: gatewayruntime.ChannelHealth{Window2StartedAt: now, Window2Requests: 6, Window2Successes: 1}},
		3: {},
	}
	ordered, _ := prioritizeAutoRouteBindingsSnapshot(bindings, nil, states, now)
	require.Equal(t, []int{3, 2, 1}, []int{ordered[0].InternalChannelID, ordered[1].InternalChannelID, ordered[2].InternalChannelID})
	states[3].user.CoolingUntil = now.Add(time.Minute)
	ordered, _ = prioritizeAutoRouteBindingsSnapshot(bindings, nil, states, now)
	require.Len(t, ordered, 3, "all-unhealthy pool still has bounded fallback candidates")
}
