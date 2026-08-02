package runtime

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"github.com/stretchr/testify/require"
)

func newUserStreamFailureContext(t *testing.T, userID int, requestID string) *gin.Context {
	t.Helper()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Set("id", userID)
	ctx.Set(constant.RequestIdKey, requestID)
	storage, err := platformhttpx.CreateBodyStorage([]byte(`{"model":"gpt-5.6-sol","prompt_cache_key":"conversation-1"}`))
	require.NoError(t, err)
	ctx.Set(platformhttpx.KeyBodyStorage, storage)
	t.Cleanup(func() { _ = storage.Close() })
	return ctx
}

func TestUserStreamFailureCircuitOpensAfterThreeIncompleteStreams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, resetUserStreamFailureCircuitForTest())
	t.Cleanup(func() { _ = resetUserStreamFailureCircuitForTest() })

	ctx := newUserStreamFailureContext(t, 42, "request-1")
	first := RecordUserIncompleteStreamFailure(ctx, "gpt-5.6-sol")
	require.False(t, first.Opened)
	require.Equal(t, 1, first.ConsecutiveFailures)

	ctx.Set(constant.RequestIdKey, "request-2")
	second := RecordUserIncompleteStreamFailure(ctx, "gpt-5.6-sol")
	require.False(t, second.Opened)
	require.Equal(t, 2, second.ConsecutiveFailures)

	ctx.Set(constant.RequestIdKey, "request-3")
	third := RecordUserIncompleteStreamFailure(ctx, "gpt-5.6-sol")
	require.True(t, third.Opened)
	require.Equal(t, userStreamFailureThreshold, third.ConsecutiveFailures)
	require.Equal(t, int(userStreamFailureCooldown.Seconds()), third.RetryAfterSeconds)

	retryAfter, blocked := UserStreamFailureRetryAfter(ctx, "gpt-5.6-sol")
	require.True(t, blocked)
	require.GreaterOrEqual(t, retryAfter, 1)
	require.LessOrEqual(t, retryAfter, int(userStreamFailureCooldown.Seconds()))
}

func TestUserStreamFailureCircuitClearsAfterSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, resetUserStreamFailureCircuitForTest())
	t.Cleanup(func() { _ = resetUserStreamFailureCircuitForTest() })

	ctx := newUserStreamFailureContext(t, 42, "request-1")
	for i := 0; i < userStreamFailureThreshold; i++ {
		ctx.Set(constant.RequestIdKey, "request-"+strconv.Itoa(i+1))
		RecordUserIncompleteStreamFailure(ctx, "gpt-5.6-sol")
	}
	_, blocked := UserStreamFailureRetryAfter(ctx, "gpt-5.6-sol")
	require.True(t, blocked)

	ClearUserStreamFailureCircuit(ctx, "gpt-5.6-sol")
	_, blocked = UserStreamFailureRetryAfter(ctx, "gpt-5.6-sol")
	require.False(t, blocked)
}
