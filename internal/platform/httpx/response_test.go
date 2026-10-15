package httpx

import (
	"bytes"
	"context"
	"fmt"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	platformtext "github.com/sh2001sh/new-api/internal/platform/textx"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestResetStatusCode(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name             string
		statusCode       int
		statusCodeConfig string
		expectedCode     int
	}{
		{
			name:             "map string value",
			statusCode:       429,
			statusCodeConfig: `{"429":"503"}`,
			expectedCode:     503,
		},
		{
			name:             "map int value",
			statusCode:       429,
			statusCodeConfig: `{"429":503}`,
			expectedCode:     503,
		},
		{
			name:             "skip invalid string value",
			statusCode:       429,
			statusCodeConfig: `{"429":"bad-code"}`,
			expectedCode:     429,
		},
		{
			name:             "skip status code 200",
			statusCode:       200,
			statusCodeConfig: `{"200":503}`,
			expectedCode:     200,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			newAPIError := &types.NewAPIError{
				StatusCode: tc.statusCode,
			}
			ResetStatusCode(newAPIError, tc.statusCodeConfig)
			require.Equal(t, tc.expectedCode, newAPIError.StatusCode)
		})
	}
}

func TestRelayErrorHandlerTruncatesInvalidJSONBodyInLog(t *testing.T) {
	withDebugEnabled(t, false)

	body := strings.Repeat("b", platformtext.LocalLogContentLimit+256)
	var logBuffer bytes.Buffer

	platformobservability.LogWriterMu.Lock()
	oldWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logBuffer
	platformobservability.LogWriterMu.Unlock()
	t.Cleanup(func() {
		platformobservability.LogWriterMu.Lock()
		gin.DefaultErrorWriter = oldWriter
		platformobservability.LogWriterMu.Unlock()
	})

	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.Equal(t, "bad response status code 500", newAPIError.Error())
	require.Contains(t, logBuffer.String(), "[truncated")
	require.Contains(t, logBuffer.String(), fmt.Sprintf("original_length=%d", len(body)))
	require.NotContains(t, logBuffer.String(), strings.Repeat("b", platformtext.LocalLogContentLimit+1))
}

func TestRelayErrorHandlerKeepsStructuredErrorMessage(t *testing.T) {
	message := strings.Repeat("c", platformtext.LocalLogContentLimit+256)
	body := `{"message":"` + message + `"}`
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.Equal(t, message, newAPIError.Error())
}

func TestRelayErrorHandlerKeepsOpenAIErrorMessage(t *testing.T) {
	message := strings.Repeat("d", platformtext.LocalLogContentLimit+256)
	body := `{"error":{"message":"` + message + `","type":"server_error","code":"server_error"}}`
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.Equal(t, message, newAPIError.Error())
}

func TestRelayErrorHandlerKeepsInvalidJSONBodyInDebugLog(t *testing.T) {
	withDebugEnabled(t, true)

	body := strings.Repeat("e", platformtext.LocalLogContentLimit+256)
	var logBuffer bytes.Buffer

	platformobservability.LogWriterMu.Lock()
	oldWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logBuffer
	platformobservability.LogWriterMu.Unlock()
	t.Cleanup(func() {
		platformobservability.LogWriterMu.Lock()
		gin.DefaultErrorWriter = oldWriter
		platformobservability.LogWriterMu.Unlock()
	})

	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.NotContains(t, logBuffer.String(), "[truncated")
	require.Contains(t, logBuffer.String(), body)
}

func TestDetectCyberPolicyError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		body    string
		matched bool
	}{
		{name: "openai error code", body: `{"error":{"message":"blocked","type":"invalid_request_error","code":"cyber_policy"}}`, matched: true},
		{name: "responses nested error", body: `{"response":{"error":{"message":"blocked","code":"cyber_policy"}}}`, matched: true},
		{name: "standard provider message", body: `{"error":{"message":"This content was flagged for possible cybersecurity risk"}}`, matched: true},
		{name: "plain standard provider message", body: `This content was flagged for possible cybersecurity risk`, matched: true},
		{name: "ordinary upstream error", body: `{"error":{"message":"rate limited","code":"rate_limit"}}`, matched: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, matched := DetectCyberPolicyError([]byte(test.body))
			require.Equal(t, test.matched, matched)
		})
	}
}

func TestRelayErrorHandlerPreservesCyberPolicyResponseAndSkipsRetry(t *testing.T) {
	t.Parallel()
	body := []byte(`{"error":{"message":"This content was flagged for possible cybersecurity risk","type":"invalid_request_error","code":"cyber_policy"}}`)
	resp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": []string{"application/json; charset=utf-8"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
	}

	apiErr := RelayErrorHandler(context.Background(), resp, false)
	require.Equal(t, types.ErrorCodeCyberPolicy, apiErr.GetErrorCode())
	require.True(t, types.IsSkipRetryError(apiErr))
	rawBody, contentType, ok := apiErr.RawResponse()
	require.True(t, ok)
	require.Equal(t, body, rawBody)
	require.Equal(t, "application/json; charset=utf-8", contentType)

	ResetStatusCode(apiErr, `{"400":503}`)
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
}

func withDebugEnabled(t *testing.T, enabled bool) {
	t.Helper()

	oldDebug := platformconfig.DebugEnabled
	platformconfig.DebugEnabled = enabled
	platformtext.SetDebugEnabled(enabled)
	t.Cleanup(func() {
		platformconfig.DebugEnabled = oldDebug
		platformtext.SetDebugEnabled(oldDebug)
	})
}
