package workflow

import (
	"errors"
	"net/http"
	"slices"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func (h *Handler) policy(w http.ResponseWriter, r *http.Request, p gateway.Principal, model string) bool {
	err := gateway.ValidateRequestPolicy(p, model, r, h.cfg.TrustedProxies)
	if err == nil && p.Group != "" && p.Group != "auto" && p.AllowedGroups != nil && !slices.Contains(p.AllowedGroups, p.Group) {
		err = errors.New("account group is not permitted")
	}
	if err == nil && h.cfg.ValidateRequest != nil {
		err = h.cfg.ValidateRequest(p, model, r)
	}
	if err == nil {
		return true
	}
	code := "task_policy_denied"
	var denied *gateway.UpstreamError
	if errors.As(err, &denied) {
		code = denied.Code
	}
	fail(w, http.StatusForbidden, code)
	return false
}
