package app

import (
	"net/http"
	"strings"

	"github.com/sh2001sh/new-api/types"
)

type upstreamFailureClass string

const (
	upstreamFailureUnknown          upstreamFailureClass = "unknown"
	upstreamFailureModelUnavailable upstreamFailureClass = "model_unavailable"
	upstreamFailureAccountExhausted upstreamFailureClass = "account_exhausted"
	upstreamFailureIncompleteStream upstreamFailureClass = "incomplete_stream"
	upstreamFailureTransient        upstreamFailureClass = "transient"
)

func classifyUpstreamFailure(err *types.NewAPIError) upstreamFailureClass {
	if err == nil {
		return upstreamFailureUnknown
	}
	if isUpstreamCapacityFailure(err) {
		return upstreamFailureTransient
	}
	if IsModelUnavailableError(err) || err.StatusCode == http.StatusServiceUnavailable {
		return upstreamFailureModelUnavailable
	}

	message := strings.ToLower(err.Error())
	if containsAny(message, "insufficient", "quota", "balance", "billing", "payment required") || err.StatusCode == http.StatusPaymentRequired {
		return upstreamFailureAccountExhausted
	}
	if isIncompleteResponsesStreamMessage(message) {
		return upstreamFailureIncompleteStream
	}
	if err.GetErrorCode() == types.ErrorCodeChannelResponseTimeExceeded ||
		err.StatusCode == http.StatusRequestTimeout ||
		err.StatusCode == http.StatusTooManyRequests ||
		err.StatusCode == http.StatusBadGateway ||
		err.StatusCode == http.StatusGatewayTimeout ||
		err.StatusCode == 524 ||
		containsAny(message, "do request failed", "response header", "stream closed", "context deadline exceeded", "i/o timeout", "connection reset") {
		return upstreamFailureTransient
	}
	return upstreamFailureUnknown
}

// isUpstreamCapacityFailure identifies a temporary provider capacity response.
// It must remain narrower than a generic 503 so account exhaustion and genuine
// model-not-found responses keep their existing model-scoped handling.
func isUpstreamCapacityFailure(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return containsAny(message,
		"selected model is at capacity",
		"model is at capacity",
		"try a different model",
		"try another model",
		"temporarily overloaded",
	)
}

// isIncompleteResponsesStreamMessage identifies a Responses API stream that
// ended after headers or deltas were sent but before its terminal event.
func isIncompleteResponsesStreamMessage(message string) bool {
	return containsAny(message,
		"stream closed before response.completed",
		"stream ended before response.completed",
		"stream terminated before response.completed",
	)
}

func containsAny(message string, terms ...string) bool {
	for _, term := range terms {
		if strings.Contains(message, term) {
			return true
		}
	}
	return false
}

// IsModelUnavailableError identifies an upstream rejection that applies to
// the requested model rather than to the entire channel credential.
func IsModelUnavailableError(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	if err.GetErrorCode() == types.ErrorCodeModelNotFound {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "model") &&
		(strings.Contains(message, "not found") || strings.Contains(message, "not exist") || strings.Contains(message, "not support") || strings.Contains(message, "unavailable"))
}

// IsModelScopedUpstreamFailure identifies failures that should isolate only
// the affected channel/model route and preserve the rest of that channel.
func IsModelScopedUpstreamFailure(err *types.NewAPIError) bool {
	class := classifyUpstreamFailure(err)
	return class == upstreamFailureModelUnavailable || class == upstreamFailureAccountExhausted
}

func isRetryableChannelFailure(err *types.NewAPIError) bool {
	if err == nil || types.IsSkipRetryError(err) {
		return false
	}
	failureClass := classifyUpstreamFailure(err)
	if failureClass == upstreamFailureIncompleteStream || failureClass == upstreamFailureTransient {
		return true
	}
	return types.IsChannelError(err) || err.StatusCode == http.StatusTooManyRequests || err.StatusCode >= http.StatusInternalServerError
}
