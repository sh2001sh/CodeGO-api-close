package app

import (
	"testing"
	"time"

	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	"github.com/stretchr/testify/require"
)

func TestRoutePoolPrefetchRefreshesNegativeLookupsOnNextRound(t *testing.T) {
	const id, model, domain = 8_210_001, "prefetch-refresh", "prefetch-refresh-provider"
	kind := gatewayruntime.RequestTypeChatLongStream
	ctx := newAutoRouteHealthContext(601)
	candidates := []gatewaystore.RoutePoolCandidate{{Channel: &gatewayschema.Channel{Id: id}, Member: gatewayschema.RoutePoolMember{CostMultiplier: 1, FaultDomain: domain}}}
	prefetchRoutePoolHealth(ctx, candidates, model, kind)
	_, found := routePoolChannelHealth(ctx, id, model, kind)
	require.False(t, found)
	for _, requestID := range []string{"refresh-1", "refresh-2"} {
		gatewayruntime.RecordUserChannelGatewayFailureForRequest(ctx, id, model, requestID, 502, kind)
	}
	gatewayruntime.RecordChannelCredentialFailure(id)
	gatewayruntime.RecordUserFaultDomainFailure(ctx, domain, model, "refresh-domain", time.Minute, kind)
	_, found = routePoolChannelHealth(ctx, id, model, kind)
	require.False(t, found, "a routing round keeps its consistent advisory snapshot")
	_, found = routePoolCredentialHealth(ctx, id)
	require.False(t, found)
	_, found = routePoolFaultDomainHealth(ctx, domain, model, kind)
	require.False(t, found)

	resetRouteHealthRequestCache(ctx)
	prefetchRoutePoolHealth(ctx, candidates, model, kind)
	health, found := routePoolChannelHealth(ctx, id, model, kind)
	require.True(t, found)
	require.Equal(t, gatewayruntime.ChannelHealthCooling, health.State)
	health, found = routePoolCredentialHealth(ctx, id)
	require.True(t, found)
	require.Equal(t, gatewayruntime.ChannelHealthCooling, health.State)
	_, found = routePoolFaultDomainHealth(ctx, domain, model, kind)
	require.True(t, found)
	_, found = routePoolChannelHealth(ctx, id, model, gatewayruntime.RequestTypeChatNonStream)
	require.False(t, found, "prefetch must not cross request types")
	_, found = routePoolChannelHealth(ctx, id, model+"-other", kind)
	require.False(t, found, "prefetch must not cross models")

	other := newAutoRouteHealthContext(602)
	prefetchRoutePoolHealth(other, candidates, model, kind)
	_, found = routePoolChannelHealth(other, id, model, kind)
	require.False(t, found, "user cooldown must stay isolated")
	_, found = routePoolFaultDomainHealth(other, domain, model, kind)
	require.False(t, found)
	_, found = routePoolCredentialHealth(other, id)
	require.True(t, found, "credential failures remain global")
}

func TestRoutePoolPreferredHealthTierPreservesSurvivorOrder(t *testing.T) {
	require.Empty(t, routePoolPreferredHealthTier(nil))
	candidates := []scoredRoutePoolCandidate{
		{channel: &gatewayschema.Channel{Id: 1}, health: gatewayruntime.ChannelHealth{State: gatewayruntime.ChannelHealthDegraded}},
		{channel: &gatewayschema.Channel{Id: 2}},
		{channel: &gatewayschema.Channel{Id: 3}, health: gatewayruntime.ChannelHealth{State: gatewayruntime.ChannelHealthDegraded}},
		{channel: &gatewayschema.Channel{Id: 4}},
	}
	preferred := routePoolPreferredHealthTier(candidates)
	require.Len(t, preferred, 2)
	require.Equal(t, 2, preferred[0].channel.Id)
	require.Equal(t, 4, preferred[1].channel.Id)
}

func TestRoutePoolCandidateSetsKeepMixedHealthClasses(t *testing.T) {
	const model = "prefetch-mixed-classes"
	kind := gatewayruntime.RequestTypeChatLongStream
	ctx := newAutoRouteHealthContext(603)
	for i := 0; i < 5; i++ {
		requestID := time.Unix(int64(i), 0).String()
		gatewayruntime.RecordUserChannelRetryableFailureForRequest(ctx, 8_210_003, model, requestID, time.Second, kind)
		gatewayruntime.RecordUserChannelRetryableFailureForRequest(ctx, 8_210_004, model, requestID, 10*time.Minute, kind)
	}
	probeHealth, found := gatewayruntime.GetUserChannelHealth(ctx, 8_210_003, model, kind)
	require.True(t, found)
	activeHealth, found := gatewayruntime.GetUserChannelHealth(ctx, 8_210_004, model, kind)
	require.True(t, found)
	now := probeHealth.CoolingUntil.Add(time.Second)
	require.True(t, activeHealth.CoolingUntil.After(now))
	gatewayruntime.RecordChannelCredentialFailure(8_210_005)
	candidates := make([]gatewaystore.RoutePoolCandidate, 0, 4)
	for id := 8_210_002; id <= 8_210_005; id++ {
		candidates = append(candidates, gatewaystore.RoutePoolCandidate{Channel: &gatewayschema.Channel{Id: id}, Member: gatewayschema.RoutePoolMember{CostMultiplier: 1}})
	}
	healthy, probes, lastResort := buildRoutePoolCandidateSets(ctx, candidates, model, kind, now)
	require.Len(t, healthy, 1)
	require.Equal(t, 8_210_002, healthy[0].channel.Id)
	require.Len(t, probes, 1)
	require.Equal(t, 8_210_003, probes[0].channel.Id)
	require.Len(t, lastResort, 1)
	require.Equal(t, 8_210_004, lastResort[0].channel.Id)
}
