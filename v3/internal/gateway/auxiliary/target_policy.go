package auxiliary

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
	var policyErr *gateway.UpstreamError
	if errors.As(err, &policyErr) {
		return policyErr
	}
	return failure(http.StatusServiceUnavailable, "target_policy_unavailable", "channel policy is unavailable")
}
