package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewaystream "github.com/sh2001sh/new-api/internal/gateway/stream"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestTemporaryServiceUnavailableClassification(t *testing.T) {
	tests := []struct {
		name    string
		message string
		status  int
		code    string
		want    upstreamFailureClass
	}{
		{"observed upstream message", "Service temporarily unavailable", 503, "", upstreamFailureTransient},
		{"status-prefixed message", "status code 503: service temporarily unavailable", 503, "", upstreamFailureTransient},
		{"chinese short", "服务暂不可用", 503, "", upstreamFailureTransient},
		{"chinese temporary", "服务暂时不可用，请稍后重试", 503, "", upstreamFailureTransient},
		{"chinese transient", "服务临时不可用", 503, "", upstreamFailureTransient},
		{"opaque 503 unchanged", "upstream timeout", 503, "", upstreamFailureModelUnavailable},
		{"empty 503 unchanged", "", 503, "", upstreamFailureModelUnavailable},
		{"unavailable accounts unchanged", "no available accounts", 503, "", upstreamFailureModelUnavailable},
		{"model not found wins", "model not found: service temporarily unavailable", 503, "", upstreamFailureModelUnavailable},
		{"unsupported model wins", "model not supported: service temporarily unavailable", 503, "", upstreamFailureModelUnavailable},
		{"model code wins", "service temporarily unavailable", 503, string(types.ErrorCodeModelNotFound), upstreamFailureModelUnavailable},
		{"account exhaustion wins", "upstream balance exhausted: service temporarily unavailable", 503, "", upstreamFailureAccountExhausted},
		{"credential rejection wins", "invalid api key: service temporarily unavailable", 503, "", upstreamFailureCredentialRejected},
		{"policy refusal wins", "content_policy_violation: service temporarily unavailable", 503, "", upstreamFailureContentPolicy},
		{"non-503 not broadened", "service temporarily unavailable", 400, "", upstreamFailureUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := types.WithOpenAIError(types.OpenAIError{Message: tt.message, Code: nil}, tt.status)
			if tt.code != "" {
				err = types.WithOpenAIError(types.OpenAIError{Message: tt.message, Code: tt.code}, tt.status)
			}
			require.Equal(t, tt.want, classifyUpstreamFailure(err))
			if tt.want == upstreamFailureTransient {
				require.False(t, IsModelScopedUpstreamFailure(err))
				require.True(t, IsRetryableChannelFailure(err))
			}
			if tt.want == upstreamFailureContentPolicy {
				require.False(t, IsRetryableChannelFailure(err))
			}
		})
	}
}

func TestTemporaryServiceUnavailableSoleRouteRetryBeforeOutput(t *testing.T) {
	original := hasAlternativeSelectableRoute
	hasAlternativeSelectableRoute = func(int, string, string) (bool, error) { return false, nil }
	t.Cleanup(func() { hasAlternativeSelectableRoute = original })

	for _, tc := range []struct {
		name              string
		stage             gatewaystream.AttemptStage
		retrySafe         bool
		responseDelivered bool
		semanticDelivered bool
		wantFallback      bool
	}{
		{"selected before output", gatewaystream.AttemptStageSelected, false, false, false, true},
		{"connected with safe Responses replay", gatewaystream.AttemptStageConnected, true, false, false, true},
		{"bootstrap with safe Responses replay", gatewaystream.AttemptStageBootstrap, true, false, false, true},
		{"connected headers alone do not permit replay", gatewaystream.AttemptStageConnected, false, false, false, false},
		{"bootstrap without safe replay", gatewaystream.AttemptStageBootstrap, false, false, false, false},
		{"after response delivery", gatewaystream.AttemptStageConnected, true, true, false, false},
		{"after semantic output", gatewaystream.AttemptStageBootstrap, true, false, true, false},
		{"semantic committed stage", gatewaystream.AttemptStageSemanticCommitted, true, false, false, false},
		{"completed stage", gatewaystream.AttemptStageCompleted, true, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			c.Set(constant.RequestIdKey, t.Name())
			c.Set("original_model", "gpt-5.6-sol")
			c.Set("use_channel", []string{"71001"})
			c.Set(string(constant.ContextKeyRelayAttemptStage), tc.stage)
			c.Set(string(constant.ContextKeyResponsesStreamRetrySafe), tc.retrySafe)
			c.Set(string(constant.ContextKeyStreamContentDelivered), tc.semanticDelivered)
			httpctx.SetContextKey(c, constant.ContextKeyResponseBodyDelivered, tc.responseDelivered)
			httpctx.SetContextKey(c, constant.ContextKeyUsingGroup, "temporary-service-sole-route")
			err := types.WithOpenAIError(types.OpenAIError{Message: "Service temporarily unavailable"}, http.StatusServiceUnavailable)
			ProcessChannelError(c, *types.NewChannelError(71001, constant.ChannelTypeOpenAI, "sole", false, "", false), err)
			if tc.wantFallback {
				require.Equal(t, 71001, httpctx.GetContextKeyInt(c, constant.ContextKeyRetryFallbackChannelID))
				require.False(t, gatewayruntime.ShouldSkipRetryAfterChannelAffinityFailure(c))
			} else {
				require.Zero(t, httpctx.GetContextKeyInt(c, constant.ContextKeyRetryFallbackChannelID))
			}
		})
	}
}
