package live

import (
	"context"
	"net/http"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

// Preserve final native signing through the production response/file wrapper.
func (p trackingProvider) FinalizeRequest(ctx context.Context, out *http.Request, req *gateway.Request, target gateway.Target) error {
	if signer, ok := p.provider.(gateway.RequestFinalizer); ok {
		return signer.FinalizeRequest(ctx, out, req, target)
	}
	return nil
}
