package live

import (
	"context"
	"errors"
	"net/http"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

// BuildRawRequest prepares real client references before the provider builds an
// endpoint template. The shared builder then restores these prepared raw bytes.
func (p trackingProvider) BuildRawRequest(ctx context.Context, req *gateway.Request, target gateway.Target) (*http.Request, error) {
	prepared, err := p.prepareTrackedRequest(ctx, req, target)
	if err != nil {
		return nil, err
	}
	out, err := gateway.BuildProviderRequest(ctx, p.provider, prepared, target)
	if err != nil {
		return nil, err
	}
	return out.WithContext(context.WithValue(out.Context(), locatorContextKey{}, target)), nil
}

func (p trackingProvider) prepareTrackedRequest(ctx context.Context, req *gateway.Request, target gateway.Target) (*gateway.Request, error) {
	body, err := p.handler.PrepareFileReferences(ctx, req, target)
	if err == nil {
		body, err = p.handler.preparePreviousResponse(ctx, req, target, body)
	}
	if err != nil {
		status := 502
		if errors.Is(err, ErrNotFound) {
			status = 404
		} else if errors.Is(err, ErrFileTooLarge) {
			status = 413
		}
		return nil, &gateway.UpstreamError{Status: status, Type: "invalid_request_error", Code: "file_reference_failed", Message: "local file reference cannot be prepared"}
	}
	prepared := *req
	prepared.Body = body
	return &prepared, nil
}
