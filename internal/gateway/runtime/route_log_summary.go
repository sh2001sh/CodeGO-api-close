package runtime

import (
	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
)

const (
	routeSummaryLogKey = "route_summary"
	adminInfoLogKey    = "admin_info"
)

// RouteLogSummary is the user-visible, identifier-free Auto routing audit.
type RouteLogSummary struct {
	Mode           string   `json:"mode"`
	CandidateCount int      `json:"candidate_count"`
	SelectedOrder  int      `json:"selected_order,omitempty"`
	SkippedCount   int      `json:"skipped_count,omitempty"`
	RetryCount     int      `json:"retry_count,omitempty"`
	Fallback       bool     `json:"fallback"`
	SkipReasons    []string `json:"skip_reasons,omitempty"`
}

// AttachRouteLogInfo adds a safe Auto summary for users and the complete
// route decision under administrator-only metadata.
func AttachRouteLogInfo(c *gin.Context, other map[string]interface{}) {
	if c == nil || other == nil {
		return
	}
	decision, hasDecision := GetRouteDecision(c)
	if hasDecision {
		// Keep retry visibility consistent across user logs without exposing
		// channel identifiers. Detailed per-channel attempts remain admin-only.
		other["attempt_count"] = len(decision.Attempts)
		other["retry_count"] = decision.RetryCount
		adminInfo, _ := other[adminInfoLogKey].(map[string]interface{})
		if adminInfo == nil {
			adminInfo = make(map[string]interface{})
		}
		adminInfo["route_decision"] = decision
		other[adminInfoLogKey] = adminInfo
	}
	if !IsAutoRouteRequest(c) {
		return
	}

	selectedOrder := selectedAutoRouteOrder(c)
	skippedCount := 0
	if selectedOrder > 1 {
		skippedCount = selectedOrder - 1
	} else if selectedOrder == 0 && decision.SelectedGroup == "" {
		skippedCount = decision.CandidateGroups
	}
	summary := RouteLogSummary{
		Mode:           "auto",
		CandidateCount: decision.CandidateGroups,
		SelectedOrder:  selectedOrder,
		SkippedCount:   skippedCount,
		RetryCount:     decision.RetryCount,
		Fallback:       decision.Fallback || selectedOrder > 1 || decision.RetryCount > 0,
		SkipReasons:    publicRouteSkipReasons(decision.Excluded),
	}
	other[routeSummaryLogKey] = summary
}

func selectedAutoRouteOrder(c *gin.Context) int {
	if _, found := httpctx.GetContextKey(c, constant.ContextKeyUnifiedAutoBindings); found {
		index := httpctx.GetContextKeyInt(c, constant.ContextKeyUnifiedAutoIndex)
		if index >= 0 {
			return index + 1
		}
		return 0
	}
	if _, found := httpctx.GetContextKey(c, constant.ContextKeyAutoGroupIndex); found {
		return httpctx.GetContextKeyInt(c, constant.ContextKeyAutoGroupIndex) + 1
	}
	return 0
}

func publicRouteSkipReasons(reasons []string) []string {
	labels := make([]string, 0, len(reasons))
	seen := make(map[string]struct{}, len(reasons))
	for _, reason := range reasons {
		label := publicRouteSkipReason(reason)
		if label == "" {
			continue
		}
		if _, exists := seen[label]; exists {
			continue
		}
		seen[label] = struct{}{}
		labels = append(labels, label)
	}
	return labels
}

func publicRouteSkipReason(reason string) string {
	switch reason {
	case "no_healthy_channel", "marketplace_auto_unavailable", "unified_auto_unavailable", "no_selectable_candidate":
		return "unavailable"
	case "channel_capacity", "fault_domain_capacity":
		return "capacity_limit"
	case "channel_concurrency_dependency":
		return "dependency_unavailable"
	case "channel_credential_cooling":
		return "credential_cooling"
	case "failed_fault_domain":
		return "failed_route"
	case "unified_auto_select_error":
		return "selection_error"
	case "unified_auto_setup_failed":
		return "setup_error"
	case "user_stream_circuit":
		return "stream_circuit"
	default:
		return ""
	}
}
