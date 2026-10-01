package native

import (
	"context"
	"errors"
	"net/http"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type requestContextKey struct{}
type preparedContextKey struct{}

type requestPolicy struct {
	request *gateway.Request
	target  gateway.Target
	clients gateway.ClientProvider
}

// WithRequest carries the original, immutable billing inputs to the native
// request builder. Provider conversion and channel overrides change only the
// outgoing HTTP request; the selected credential also owns its transport.
func WithRequest(ctx context.Context, req *gateway.Request, target gateway.Target, clients gateway.ClientProvider) context.Context {
	return context.WithValue(ctx, requestContextKey{}, requestPolicy{req, target, clients})
}

// WithTarget preserves original request metadata and the selected client while
// allowing adapters to add the channel context when called directly.
func WithTarget(ctx context.Context, target gateway.Target) context.Context {
	policy, _ := ctx.Value(requestContextKey{}).(requestPolicy)
	policy.target = target
	if policy.request == nil {
		policy.request = &gateway.Request{Model: target.UpstreamModel}
	}
	return context.WithValue(ctx, requestContextKey{}, policy)
}

// Prepare must run after conversion and before native signing. JSON and Do
// invoke it automatically; signers invoke it explicitly so they sign the final
// bytes. A marker avoids applying operations twice after signing.
func Prepare(req *http.Request) error {
	if prepared, _ := req.Context().Value(preparedContextKey{}).(bool); prepared {
		return nil
	}
	policy, ok := req.Context().Value(requestContextKey{}).(requestPolicy)
	if !ok {
		return nil
	}
	target := policy.target
	if req.Method == http.MethodGet || req.Method == http.MethodHead {
		// Channel body parameters belong to JSON operations, not bodyless reads.
		target.ParamOverride = nil
	}
	if req.Body == nil || req.Body == http.NoBody {
		req.Header.Del("Content-Type")
	}
	if err := gateway.ApplyUpstreamRequest(req, policy.request, target); err != nil {
		return &InvalidRequest{Err: err}
	}
	*req = *req.WithContext(context.WithValue(req.Context(), preparedContextKey{}, true))
	return nil
}

// Do applies channel policy once and maps status before provider classifiers.
func Do(client *http.Client, req *http.Request) (*http.Response, error) {
	if err := Prepare(req); err != nil {
		return nil, err
	}
	resp, err := Client(client).Do(req)
	if err != nil {
		return nil, errors.New("task provider transport failed")
	}
	if policy, ok := req.Context().Value(requestContextKey{}).(requestPolicy); ok {
		resp.StatusCode = gateway.MapUpstreamStatus(resp.StatusCode, policy.target)
	}
	return resp, nil
}
