package runtime

import (
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/stretchr/testify/require"
)

func TestUserRouteHealthIsolatesUsersAndRequestTypes(t *testing.T) {
	require.NoError(t, resetChannelHealthForTest())
	t.Cleanup(func() { require.NoError(t, resetChannelHealthForTest()) })

	const (
		channelID = 8_100_001
		modelName = "gpt-user-route-isolation"
	)
	userA := newUserRouteHealthContext(101)
	userB := newUserRouteHealthContext(202)
	for _, requestID := range []string{"request-a-1", "request-a-2"} {
		RecordUserChannelGatewayFailureForRequest(userA, channelID, modelName, requestID, 502, RequestTypeChatShortStream)
	}

	state, found := GetUserChannelHealth(userA, channelID, modelName, RequestTypeChatShortStream)
	require.True(t, found)
	require.Equal(t, ChannelHealthCooling, state.State)
	require.True(t, state.CoolingUntil.After(time.Now()))

	_, found = GetUserChannelHealth(userB, channelID, modelName, RequestTypeChatShortStream)
	require.False(t, found)
	_, found = GetUserChannelHealth(userA, channelID, modelName, RequestTypeChatLongStream)
	require.False(t, found)
	_, found = GetChannelHealth(channelID, modelName, RequestTypeChatShortStream)
	require.False(t, found)
}

func TestIsAutoRouteRequestSurvivesUnifiedBindingGroupRewrite(t *testing.T) {
	context := newUserRouteHealthContext(102)
	httpctx.SetContextKey(context, constant.ContextKeyTokenGroup, "internal-marketplace-group")
	httpctx.SetContextKey(context, constant.ContextKeyUnifiedAutoBindings, []int{1})
	require.True(t, IsAutoRouteRequest(context))

	httpctx.SetContextKey(context, constant.ContextKeyUnifiedAutoBindings, nil)
	require.True(t, IsAutoRouteRequest(context))

	marked, _ := gin.CreateTestContext(httptest.NewRecorder())
	httpctx.SetContextKey(marked, constant.ContextKeyTokenGroup, "internal-marketplace-group")
	MarkAutoRouteRequest(marked)
	require.True(t, IsAutoRouteRequest(marked))

	ordinary, _ := gin.CreateTestContext(httptest.NewRecorder())
	httpctx.SetContextKey(ordinary, constant.ContextKeyTokenGroup, "default")
	require.False(t, IsAutoRouteRequest(ordinary))
}

func TestUserRouteHealthRecoveryNeedsTwoSuccessfulProbes(t *testing.T) {
	require.NoError(t, resetChannelHealthForTest())
	t.Cleanup(func() { require.NoError(t, resetChannelHealthForTest()) })

	const (
		channelID = 8_100_002
		modelName = "gpt-user-route-recovery"
	)
	context := newUserRouteHealthContext(303)
	for _, requestID := range []string{"failure-1", "failure-2"} {
		RecordUserChannelGatewayFailureForRequest(context, channelID, modelName, requestID, 502, RequestTypeChatShortStream)
	}

	require.True(t, TryStartUserChannelLastResortProbe(context, channelID, modelName, RequestTypeChatShortStream))
	require.False(t, TryStartUserChannelLastResortProbe(context, channelID, modelName, RequestTypeChatShortStream))
	RecordUserChannelSuccess(context, channelID, modelName, 0, RequestTypeChatShortStream)

	state, found := GetUserChannelHealth(context, channelID, modelName, RequestTypeChatShortStream)
	require.True(t, found)
	require.Equal(t, ChannelHealthCooling, state.State)
	require.Equal(t, 1, state.RecoveryProbeSuccesses)

	require.True(t, TryStartUserChannelLastResortProbe(context, channelID, modelName, RequestTypeChatShortStream))
	RecordUserChannelSuccess(context, channelID, modelName, 0, RequestTypeChatShortStream)
	_, found = GetUserChannelHealth(context, channelID, modelName, RequestTypeChatShortStream)
	require.False(t, found)
}

func TestUserRouteHealthProbeLeaseAllowsOneConcurrentRequest(t *testing.T) {
	require.NoError(t, resetChannelHealthForTest())
	t.Cleanup(func() { require.NoError(t, resetChannelHealthForTest()) })

	const (
		channelID = 8_100_004
		modelName = "gpt-user-route-concurrent-probe"
	)
	context := newUserRouteHealthContext(405)
	for _, requestID := range []string{"failure-1", "failure-2"} {
		RecordUserChannelGatewayFailureForRequest(context, channelID, modelName, requestID, 502, RequestTypeChatShortStream)
	}

	start := make(chan struct{})
	var winners atomic.Int32
	var waitGroup sync.WaitGroup
	for range 32 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			if TryStartUserChannelLastResortProbe(context, channelID, modelName, RequestTypeChatShortStream) {
				winners.Add(1)
			}
		}()
	}
	close(start)
	waitGroup.Wait()
	require.Equal(t, int32(1), winners.Load())
}

func TestUserRouteHealthSuccessDecaysFailureHistory(t *testing.T) {
	require.NoError(t, resetChannelHealthForTest())
	t.Cleanup(func() { require.NoError(t, resetChannelHealthForTest()) })

	const (
		channelID = 8_100_003
		modelName = "gpt-user-route-decay"
	)
	context := newUserRouteHealthContext(404)
	RecordUserChannelRetryableFailureForRequest(context, channelID, modelName, "failure-1", 15*time.Second, RequestTypeChatNonStream)
	RecordUserChannelRetryableFailureForRequest(context, channelID, modelName, "failure-2", 15*time.Second, RequestTypeChatNonStream)
	RecordUserChannelSuccess(context, channelID, modelName, 0, RequestTypeChatNonStream)

	state, found := GetUserChannelHealth(context, channelID, modelName, RequestTypeChatNonStream)
	require.True(t, found)
	require.Equal(t, ChannelHealthDegraded, state.State)
	require.Equal(t, 1, state.ConsecutiveRetryableFailures)
}

func TestUserFaultDomainHealthIsolatesUsersAndRequestTypes(t *testing.T) {
	require.NoError(t, resetChannelHealthForTest())
	t.Cleanup(func() { require.NoError(t, resetChannelHealthForTest()) })

	const (
		domain    = "1:user-route.example"
		modelName = "gpt-user-domain-isolation"
	)
	userA := newUserRouteHealthContext(701)
	userB := newUserRouteHealthContext(702)
	for _, requestID := range []string{"domain-1", "domain-2", "domain-3"} {
		RecordUserFaultDomainFailure(userA, domain, modelName, requestID, 15*time.Second, RequestTypeChatShortStream)
	}

	state, found := GetUserFaultDomainHealth(userA, domain, modelName, RequestTypeChatShortStream)
	require.True(t, found)
	require.Equal(t, ChannelHealthCooling, state.State)
	_, found = GetUserFaultDomainHealth(userB, domain, modelName, RequestTypeChatShortStream)
	require.False(t, found)
	_, found = GetUserFaultDomainHealth(userA, domain, modelName, RequestTypeChatLongStream)
	require.False(t, found)
	_, found = GetFaultDomainHealth(domain, modelName, RequestTypeChatShortStream)
	require.False(t, found)
}

func newUserRouteHealthContext(userID int) *gin.Context {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	httpctx.SetContextKey(context, constant.ContextKeyUserId, userID)
	httpctx.SetContextKey(context, constant.ContextKeyTokenGroup, "auto")
	return context
}
