package app

import (
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	platformhttpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
)

// OrderAutoGroups returns configured automatic groups in user preference order,
// with only this user's actively cooling routes stably demoted.
func OrderAutoGroups(c *gin.Context, userGroup, model string) []string {
	groups := GetUserAutoGroup(userGroup)
	return orderGroupsByAutoPolicy(c, userGroup, model, groups, time.Now())
}

// OrderAutoFallbackGroups returns permitted groups outside the configured Auto
// list. They have no configured rank, so effective cost is their deterministic
// preference while user-scoped cooling still demotes unhealthy fallbacks.
func OrderAutoFallbackGroups(c *gin.Context, userGroup, model string, excluded []string) []string {
	excludedSet := make(map[string]struct{}, len(excluded))
	for _, group := range excluded {
		excludedSet[group] = struct{}{}
	}

	usableGroups := GetUserUsableGroups(userGroup)
	groups := make([]string, 0, len(usableGroups))
	for group := range usableGroups {
		if group == AutoGroupName {
			continue
		}
		if _, exists := excludedSet[group]; exists {
			continue
		}
		groups = append(groups, group)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		left := GetUserGroupRatio(userGroup, groups[i])
		right := GetUserGroupRatio(userGroup, groups[j])
		if left == right {
			return groups[i] < groups[j]
		}
		return left < right
	})
	return orderGroupsByAutoPolicy(c, userGroup, model, groups, time.Now())
}

func orderGroupsByAutoPolicy(c *gin.Context, _ string, model string, groups []string, now time.Time) []string {
	preferred := make([]string, 0, len(groups))
	cooling := make([]string, 0, len(groups))
	for _, group := range groups {
		switch {
		case isAutoGroupCooling(c, group, model, now):
			cooling = append(cooling, group)
		case autoGroupNeedsRecoveryProbe(c, group, model, now):
			if tryStartAutoGroupRecoveryProbe(c, group, model, now) {
				preferred = append(preferred, group)
			} else {
				cooling = append(cooling, group)
			}
		default:
			preferred = append(preferred, group)
		}
	}
	return append(preferred, cooling...)
}

func selectedAutoGroup(ctx *gin.Context) string {
	if ctx == nil || platformhttpctx.GetContextKeyString(ctx, constant.ContextKeyTokenGroup) != AutoGroupName {
		return ""
	}
	return platformhttpctx.GetContextKeyString(ctx, constant.ContextKeyAutoGroup)
}

// RecordAutoGroupSuccess advances only the current user's group/model circuit.
func RecordAutoGroupSuccess(ctx *gin.Context, model string) {
	recordAutoGroupSuccess(ctx, selectedAutoGroup(ctx), model, time.Now())
}

// RecordAutoGroupFailure advances only the current user's group/model circuit.
func RecordAutoGroupFailure(ctx *gin.Context, model string) {
	recordAutoGroupFailure(ctx, selectedAutoGroup(ctx), model, time.Now())
}
