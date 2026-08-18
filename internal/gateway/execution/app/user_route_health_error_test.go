package app

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestAutoTransientFailureDoesNotMutateGlobalChannelHealth(t *testing.T) {
	const (
		channelID = 8_300_001
		modelName = "gpt-auto-transient-user-only"
	)
	context := newExecutionAutoHealthContext(601, "request-1", modelName)
	err := types.NewOpenAIError(errors.New("upstream bad gateway"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)
	recordChannelTransientFailure(context, channelID, modelName, err)
	recordChannelTransientFailure(newExecutionAutoHealthContext(601, "request-2", modelName), channelID, modelName, err)

	userState, found := gatewayruntime.GetUserChannelHealth(context, channelID, modelName, gatewayruntime.RequestTypeOther)
	require.True(t, found)
	require.Equal(t, gatewayruntime.ChannelHealthCooling, userState.State)
	sharedState, found := gatewayruntime.GetChannelHealth(channelID, modelName, gatewayruntime.RequestTypeOther)
	require.True(t, found)
	require.Empty(t, sharedState.State)
	require.Zero(t, sharedState.ConsecutiveRetryableFailures)
	require.Equal(t, 2, sharedState.Window2Requests)
}

func TestAnonymousAutoTransientFailureDoesNotCreateSharedHealth(t *testing.T) {
	const (
		channelID = 8_300_003
		modelName = "gpt-anonymous-auto-transient"
	)
	context := newExecutionAutoHealthContext(0, "anonymous-1", modelName)
	err := types.NewOpenAIError(errors.New("upstream bad gateway"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)
	recordChannelTransientFailure(context, channelID, modelName, err)
	context.Set(constant.RequestIdKey, "anonymous-2")
	recordChannelTransientFailure(context, channelID, modelName, err)

	_, found := gatewayruntime.GetUserChannelHealth(context, channelID, modelName, gatewayruntime.RequestTypeOther)
	require.False(t, found)
	sharedState, found := gatewayruntime.GetChannelHealth(channelID, modelName, gatewayruntime.RequestTypeOther)
	require.True(t, found)
	require.Empty(t, sharedState.State)
	require.Zero(t, sharedState.ConsecutiveRetryableFailures)
	require.Equal(t, 2, sharedState.Window2Requests)
}

func TestNonAutoTransientFailureRetainsGlobalHealth(t *testing.T) {
	const (
		channelID = 8_300_004
		modelName = "gpt-non-auto-transient"
	)
	context := newExecutionAutoHealthContext(603, "non-auto-1", modelName)
	httpctx.SetContextKey(context, constant.ContextKeyTokenGroup, "default")
	err := types.NewOpenAIError(errors.New("upstream bad gateway"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)
	recordChannelTransientFailure(context, channelID, modelName, err)
	context.Set(constant.RequestIdKey, "non-auto-2")
	recordChannelTransientFailure(context, channelID, modelName, err)

	state, found := gatewayruntime.GetChannelHealth(channelID, modelName, gatewayruntime.RequestTypeOther)
	require.True(t, found)
	require.Equal(t, gatewayruntime.ChannelHealthCooling, state.State)
	_, found = gatewayruntime.GetUserChannelHealth(context, channelID, modelName, gatewayruntime.RequestTypeOther)
	require.False(t, found)
}

func TestAutoCredentialRejectionStillUsesGlobalCredentialCircuit(t *testing.T) {
	const channelID = 8_300_002
	context := newExecutionAutoHealthContext(602, "credential-request", "gpt-auto-credential")
	ProcessChannelError(
		context,
		*types.NewChannelError(channelID, constant.ChannelTypeOpenAI, "credential-route", false, "", false),
		types.NewOpenAIError(errors.New("invalid API key"), types.ErrorCodeBadResponseStatusCode, http.StatusUnauthorized),
	)

	require.True(t, gatewayruntime.IsChannelCredentialCooling(channelID))
}

func newExecutionAutoHealthContext(userID int, requestID, modelName string) *gin.Context {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	httpctx.SetContextKey(context, constant.ContextKeyUserId, userID)
	httpctx.SetContextKey(context, constant.ContextKeyTokenGroup, "auto")
	context.Set(constant.RequestIdKey, requestID)
	context.Set("original_model", modelName)
	return context
}
