package app

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/stretchr/testify/require"
)

func TestRoutePoolUserCoolingDoesNotAffectAnotherUser(t *testing.T) {
	const (
		channelID = 8_200_001
		modelName = "gpt-route-pool-user-isolation"
	)
	userA := newAutoRouteHealthContext(501)
	userB := newAutoRouteHealthContext(502)
	for _, requestID := range []string{"user-a-1", "user-a-2"} {
		gatewayruntime.RecordUserChannelGatewayFailureForRequest(userA, channelID, modelName, requestID, 502, gatewayruntime.RequestTypeChatShortStream)
	}
	candidates := []gatewaystore.RoutePoolCandidate{{
		Channel: &gatewayschema.Channel{Id: channelID},
		Member:  gatewayschema.RoutePoolMember{CostMultiplier: 0.09},
	}}

	healthyA, probesA, lastResortA := buildRoutePoolCandidateSets(userA, candidates, modelName, gatewayruntime.RequestTypeChatShortStream, time.Now())
	require.Empty(t, healthyA)
	require.Empty(t, probesA)
	require.Len(t, lastResortA, 1)

	healthyB, probesB, lastResortB := buildRoutePoolCandidateSets(userB, candidates, modelName, gatewayruntime.RequestTypeChatShortStream, time.Now())
	require.Len(t, healthyB, 1)
	require.Empty(t, probesB)
	require.Empty(t, lastResortB)
}

func TestRoutePoolAllUserCandidatesCoolingKeepsOneLastResortProbe(t *testing.T) {
	const (
		channelID = 8_200_002
		modelName = "gpt-route-pool-user-last-resort"
	)
	context := newAutoRouteHealthContext(503)
	for _, requestID := range []string{"failure-1", "failure-2"} {
		gatewayruntime.RecordUserChannelGatewayFailureForRequest(context, channelID, modelName, requestID, 502, gatewayruntime.RequestTypeChatShortStream)
	}
	candidates := []gatewaystore.RoutePoolCandidate{{
		Channel: &gatewayschema.Channel{Id: channelID},
		Member:  gatewayschema.RoutePoolMember{CostMultiplier: 0.09},
	}}
	_, _, lastResort := buildRoutePoolCandidateSets(context, candidates, modelName, gatewayruntime.RequestTypeChatShortStream, time.Now())

	probe := reserveRoutePoolLastResortProbe(context, lastResort, modelName, gatewayruntime.RequestTypeChatShortStream)
	require.NotNil(t, probe)
	require.Equal(t, channelID, probe.channel.Id)
	require.Nil(t, reserveRoutePoolLastResortProbe(context, lastResort, modelName, gatewayruntime.RequestTypeChatShortStream))
}

func TestRoutePoolCredentialConflictReleasesUserChannelProbe(t *testing.T) {
	const (
		channelID = 8_200_003
		modelName = "gpt-route-pool-partial-probe-release"
	)
	context := newAutoRouteHealthContext(504)
	for _, requestID := range []string{"failure-1", "failure-2"} {
		gatewayruntime.RecordUserChannelGatewayFailureForRequest(context, channelID, modelName, requestID, 502, gatewayruntime.RequestTypeChatShortStream)
	}
	gatewayruntime.RecordChannelCredentialFailure(channelID)
	candidate := []scoredRoutePoolCandidate{{
		channel:         &gatewayschema.Channel{Id: channelID},
		channelProbe:    true,
		credentialProbe: true,
	}}

	require.Nil(t, reserveRoutePoolLastResortProbe(context, candidate, modelName, gatewayruntime.RequestTypeChatShortStream))
	require.True(t, gatewayruntime.TryStartUserChannelLastResortProbe(context, channelID, modelName, gatewayruntime.RequestTypeChatShortStream))
}

func TestAutoRouteIgnoresSharedCooldownAsHardExclusion(t *testing.T) {
	const (
		channelID = 8_200_004
		modelName = "gpt-route-pool-shared-soft-only"
	)
	for _, requestID := range []string{"shared-1", "shared-2"} {
		gatewayruntime.RecordChannelGatewayFailureForRequest(channelID, modelName, requestID, 502, gatewayruntime.RequestTypeChatShortStream)
	}
	candidates := []gatewaystore.RoutePoolCandidate{{
		Channel: &gatewayschema.Channel{Id: channelID},
		Member:  gatewayschema.RoutePoolMember{CostMultiplier: 0.09},
	}}

	autoContext := newAutoRouteHealthContext(505)
	healthy, probes, lastResort := buildRoutePoolCandidateSets(autoContext, candidates, modelName, gatewayruntime.RequestTypeChatShortStream, time.Now())
	require.Len(t, healthy, 1)
	require.Empty(t, probes)
	require.Empty(t, lastResort)
	require.Greater(t, healthy[0].score, healthy[0].cost)

	nonAutoContext := newAutoRouteHealthContext(506)
	httpctx.SetContextKey(nonAutoContext, constant.ContextKeyTokenGroup, "default")
	healthy, probes, lastResort = buildRoutePoolCandidateSets(nonAutoContext, candidates, modelName, gatewayruntime.RequestTypeChatShortStream, time.Now())
	require.Empty(t, healthy)
	require.Empty(t, probes)
	require.Len(t, lastResort, 1)
}

func newAutoRouteHealthContext(userID int) *gin.Context {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	httpctx.SetContextKey(context, constant.ContextKeyUserId, userID)
	httpctx.SetContextKey(context, constant.ContextKeyTokenGroup, AutoGroupName)
	return context
}
