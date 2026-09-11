package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestFinalizeRelayErrorForwardsRawCyberPolicyResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"error":{"message":"This content was flagged for possible cybersecurity risk","code":"cyber_policy"}}`)
	apiErr := types.WithOpenAIError(types.OpenAIError{
		Message: "This content was flagged for possible cybersecurity risk",
		Code:    types.ErrorCodeCyberPolicy,
	}, http.StatusForbidden, types.ErrOptionWithSkipRetry())
	apiErr.SetRawResponse(body, "application/json")

	finalizeRelayError(c, types.RelayFormatOpenAI, nil, apiErr, "")

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Equal(t, body, recorder.Body.Bytes())
	require.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	require.False(t, shouldCountRelayFailureInSuccessRate(apiErr))
}
