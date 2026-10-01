package workflow

import (
	"context"
	"net/http"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func (h *Handler) fetchResult(ctx context.Context, raw string, target gateway.Target) (*http.Response, error) {
	return native.Media(ctx, &http.Client{Timeout: h.cfg.ReconcileTimeout}, target, raw, nil)
}
