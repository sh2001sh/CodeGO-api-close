package gateway

import (
	"context"
	"errors"
	"net/http"
)

// FinalizeProviderRequest preserves final-byte signing through protocol wrappers.
func FinalizeProviderRequest(ctx context.Context, provider Provider, out *http.Request, req *Request, target Target) error {
	if finalizer, ok := provider.(RequestFinalizer); ok {
		return finalizer.FinalizeRequest(ctx, out, req, target)
	}
	return nil
}

func closeRequestBody(req *http.Request) {
	if req.Body != nil {
		_ = req.Body.Close()
	}
}

func requestBuildFailure(err error) AttemptResult {
	var upstreamErr *UpstreamError
	if errors.As(err, &upstreamErr) {
		result := classifyStatus(upstreamErr.Status, nil)
		result.Err = upstreamErr
		var intentional *ParamOverrideReturnError
		if errors.As(err, &intentional) && intentional.SkipRetry {
			result.Retryable, result.Scope = false, ScopeRequest
		}
		return result
	}
	return AttemptResult{Retryable: true, Scope: ScopeCredential,
		Err: &UpstreamError{Status: http.StatusBadGateway, Type: "upstream_error", Code: "request_build_failed", Message: "could not build upstream request"}}
}
