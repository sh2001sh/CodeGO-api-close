package http

import (
	"strconv"

	"github.com/gin-gonic/gin"
	communityapp "github.com/sh2001sh/new-api/internal/community/app"
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
)

func listApprovedResources(c *gin.Context) {
	result, err := communityapp.ListApprovedResources(parseResourceListRequest(c))
	respondResourceList(c, result, err)
}

func listMyResources(c *gin.Context) {
	result, err := communityapp.ListUserResources(c.GetInt("id"), parseResourceListRequest(c))
	respondResourceList(c, result, err)
}

func listAdminResources(c *gin.Context) {
	result, err := communityapp.ListAdminResources(parseResourceListRequest(c))
	respondResourceList(c, result, err)
}

func getResourceConfig(c *gin.Context) {
	httpapi.ApiSuccess(c, communityapp.GetResourceConfig())
}

func updateResourceConfig(c *gin.Context) {
	var request communityapp.UpdateResourceConfigRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		httpapi.ApiErrorMsg(c, "invalid community resource configuration")
		return
	}
	config, err := communityapp.UpdateResourceConfig(request)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, config)
}

func createResource(c *gin.Context) {
	var request communityapp.CreateResourceRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		httpapi.ApiErrorMsg(c, "invalid community resource request")
		return
	}
	resource, err := communityapp.CreateResource(c.GetInt("id"), c.GetString("username"), c.GetInt("role"), request)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, resource)
}

func reviewResource(c *gin.Context) {
	resourceID, err := communityapp.ParseResourceID(c.Param("id"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	var request communityapp.ReviewResourceRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		httpapi.ApiErrorMsg(c, "invalid community resource review")
		return
	}
	resource, err := communityapp.ReviewResource(resourceID, c.GetInt("id"), request)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, resource)
}

func parseResourceListRequest(c *gin.Context) communityapp.ListResourcesRequest {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	return communityapp.ListResourcesRequest{
		Keyword:  c.Query("keyword"),
		Category: c.Query("category"),
		Status:   c.Query("status"),
		Page:     page,
		PageSize: pageSize,
	}
}

func respondResourceList(c *gin.Context, result *communityapp.ResourceList, err error) {
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, result)
}
