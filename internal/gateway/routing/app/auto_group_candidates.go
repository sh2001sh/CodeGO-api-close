package app

import (
	"github.com/gin-gonic/gin"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	"github.com/sh2001sh/new-api/internal/platform/logger"
)

var hasSelectableAutoGroupRoute = selectableAutoGroupRoute

func selectableAutoGroupRoute(group, modelName string) (bool, error) {
	detail, err := gatewaystore.LoadEnabledRoutePool(group)
	if err != nil {
		return false, err
	}
	if detail == nil {
		return gatewaystore.HasEnabledChannelForGroupModel(group, modelName), nil
	}
	candidates, err := gatewaystore.LoadRoutePoolCandidates(group, modelName, detail)
	return len(candidates) > 0, err
}

func markRemainingAutoGroupRoutes(c *gin.Context, groups []string, current int, modelName string) {
	for index := current + 1; index < len(groups); index++ {
		selectable, err := hasSelectableAutoGroupRoute(groups[index], modelName)
		if err != nil {
			logger.LogError(c, "check remaining auto group route failed: "+err.Error())
			gatewayruntime.MarkRemainingCrossGroupRoutes(c, 1)
			return
		}
		if selectable {
			gatewayruntime.MarkRemainingCrossGroupRoutes(c, 1)
			return
		}
	}
	gatewayruntime.MarkRemainingCrossGroupRoutes(c, 0)
}
