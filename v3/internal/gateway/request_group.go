package gateway

import (
	"net/http"
	"slices"
	"strings"
)

// requestedPrincipal applies a request-local route selection after key auth.
// The identity grants and the planner's current market/pool checks still apply;
// neither the key profile nor its persistent default group is changed.
func requestedPrincipal(principal Principal, request *http.Request) (Principal, error) {
	values := request.Header.Values("X-CodeGo-Group")
	if len(values) == 0 {
		return principal, nil
	}
	if len(values) != 1 || values[0] == "" || len(values[0]) > 128 || strings.TrimSpace(values[0]) != values[0] || strings.ContainsAny(values[0], ",\r\n") {
		return Principal{}, &UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error",
			Code: "invalid_group", Message: "provide one valid route group"}
	}
	group := values[0]
	if !slices.Contains(principal.AllowedGroups, group) {
		return Principal{}, &UpstreamError{Status: http.StatusForbidden, Type: "permission_error",
			Code: "group_not_allowed", Message: "API key does not allow this group"}
	}
	principal.Group = group
	// An explicit selection must not silently retry through a different group.
	principal.CrossGroupRetry = false
	return principal, nil
}
