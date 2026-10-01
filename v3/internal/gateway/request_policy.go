package gateway

import (
	"net/http"
	"net/netip"
	"slices"
)

// ValidateRequestPolicy enforces API Key model and network permissions across
// text, media, WebSocket and asynchronous APIs. Pass the requested public model,
// before mapping or billing suffixes. Empty model is for non-generation file
// and response metadata operations; network restrictions still apply.
func ValidateRequestPolicy(principal Principal, model string, request *http.Request, trustedProxies []netip.Prefix) error {
	// Virtual card groups are resolved and entitlement-checked by the planner.
	virtualGroup := principal.Group == "zero-hour" || principal.Group == "monthly-pass"
	if principal.Group != "" && principal.Group != "auto" && !virtualGroup && principal.AllowedGroups != nil && !slices.Contains(principal.AllowedGroups, principal.Group) {
		return &UpstreamError{Status: http.StatusForbidden, Type: "permission_error",
			Code: "group_not_allowed", Message: "API key does not allow this group"}
	}
	if model != "" && principal.AllowedModels != nil && !slices.Contains(principal.AllowedModels, model) {
		return &UpstreamError{Status: http.StatusForbidden, Type: "permission_error",
			Code: "model_not_allowed", Message: "API key does not allow this model"}
	}
	if principal.AllowedCIDRs == nil {
		return nil
	}
	if request == nil {
		return networkDenied()
	}
	address := (&Gateway{cfg: Config{TrustedProxies: trustedProxies}}).clientAddress(request)
	ip, err := netip.ParseAddr(address)
	if err != nil {
		return networkDenied()
	}
	ip = ip.Unmap()
	for _, prefix := range principal.AllowedCIDRs {
		if prefix.Contains(ip) {
			return nil
		}
	}
	return networkDenied()
}

func networkDenied() error {
	return &UpstreamError{Status: http.StatusForbidden, Type: "permission_error",
		Code: "ip_not_allowed", Message: "API key does not allow this client address"}
}
