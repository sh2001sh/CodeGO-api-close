package http

import (
	"github.com/gin-gonic/gin"
	gatewayroutingapp "github.com/sh2001sh/new-api/internal/gateway/routing/app"
	identityapp "github.com/sh2001sh/new-api/internal/identity/app"
	marketplaceapp "github.com/sh2001sh/new-api/internal/marketplace/app"
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
	"sort"
	"strings"
)

func GetUserSelf(c *gin.Context) {
	payload, err := identityapp.GetSelfProfile(c.GetInt("id"), c.GetInt("role"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func GetUserModels(c *gin.Context) {
	group := strings.TrimSpace(c.Query("group"))
	if marketplaceapp.IsMarketplaceRoutePoolTokenGroup(group) {
		models, err := marketplaceapp.ListRoutePoolModels(c.GetInt("id"), marketplaceapp.RoutePoolIDFromTokenGroup(group))
		if err != nil {
			httpapi.ApiError(c, err)
			return
		}
		httpapi.ApiSuccess(c, models)
		return
	}
	if marketplaceapp.IsMarketplaceTokenGroup(group) && !marketplaceapp.IsMarketplaceAutoTokenGroup(group) {
		binding, err := marketplaceapp.ResolveTokenGroupBinding(group, c.GetInt("id"))
		if err != nil {
			httpapi.ApiError(c, err)
			return
		}
		httpapi.ApiSuccess(c, binding.Models)
		return
	}
	if (group == gatewayroutingapp.AutoGroupName && marketplaceapp.HasConfiguredAutoRoutePool(c.GetInt("id"))) || marketplaceapp.IsMarketplaceAutoTokenGroup(group) {
		pool, err := marketplaceapp.ListAutoRoutePool(c.GetInt("id"))
		if err != nil {
			httpapi.ApiError(c, err)
			return
		}
		set := make(map[string]struct{})
		for _, item := range pool.Items {
			if !item.Selected {
				continue
			}
			for _, model := range item.Models {
				set[model] = struct{}{}
			}
		}
		models := make([]string, 0, len(set))
		for model := range set {
			models = append(models, model)
		}
		sort.Strings(models)
		httpapi.ApiSuccess(c, models)
		return
	}
	models, err := identityapp.ListUserModelsForGroup(c.GetInt("id"), group)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, models)
}

func GetUserAffiliateCode(c *gin.Context) {
	code, err := identityapp.EnsureAffiliateCode(c.GetInt("id"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, code)
}

func GetUserAffiliateRewardsOverview(c *gin.Context) {
	overview, err := identityapp.LoadAffiliateRewardsOverview(c.GetInt("id"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, overview)
}
