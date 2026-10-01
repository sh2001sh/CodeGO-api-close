package live

import (
	"errors"
	"net/http"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func (h *Handler) targetPolicyFailure(req *gateway.Request, target gateway.Target) *gateway.UpstreamError {
	if h.cfg.TargetPolicy == nil {
		return nil
	}
	err := h.cfg.TargetPolicy(req, target)
	if err == nil {
		return nil
	}
	var failure *gateway.UpstreamError
	if errors.As(err, &failure) {
		return failure
	}
	return &gateway.UpstreamError{Status: http.StatusForbidden, Type: "permission_error", Code: "request_not_permitted", Message: err.Error()}
}
