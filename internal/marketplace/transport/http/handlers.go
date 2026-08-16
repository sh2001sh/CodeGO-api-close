package http

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	marketplaceapp "github.com/sh2001sh/new-api/internal/marketplace/app"
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
)

func ListGroups(c *gin.Context) {
	query := marketplaceapp.GroupQuery{
		ViewerUserID: c.GetInt("id"),
		Search:       c.Query("search"), Model: c.Query("model"), Source: c.Query("source"),
		Provider: c.Query("provider"), Status: c.Query("status"),
		Verification: c.Query("verification"), Sort: c.Query("sort"), Direction: c.Query("direction"),
		WindowHours: queryInt(c, "window_hours", 24), Page: queryInt(c, "page", 1),
		PageSize: queryInt(c, "page_size", 20), MinMultiplier: queryFloat(c, "min_multiplier"),
		MaxMultiplier: queryFloat(c, "max_multiplier"),
	}
	result, err := marketplaceapp.ListMarketplaceGroups(query)
	respond(c, result, err)
}

func GetGroup(c *gin.Context) {
	result, err := marketplaceapp.GetMarketplaceGroup(c.Param("slug"), queryInt(c, "window_hours", 24), c.GetInt("id"))
	respond(c, result, err)
}

func SubmitChannelFeedback(c *gin.Context) {
	var req marketplaceapp.ChannelFeedbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	result, err := marketplaceapp.SubmitChannelFeedback(c.GetInt("id"), c.Param("id"), req)
	respond(c, result, err)
}

func GetAutoRoutePool(c *gin.Context) {
	result, err := marketplaceapp.ListAutoRoutePool(c.GetInt("id"))
	respond(c, result, err)
}

func UpdateAutoRoutePool(c *gin.Context) {
	var req marketplaceapp.AutoRoutePoolUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	result, err := marketplaceapp.ReplaceAutoRoutePool(c.GetInt("id"), req)
	respond(c, result, err)
}

func CreateChannel(c *gin.Context) {
	var req marketplaceapp.CreateChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	result, err := marketplaceapp.CreateMarketplaceChannel(c.GetInt("id"), req)
	respond(c, result, err)
}

func FetchModels(c *gin.Context) {
	var req marketplaceapp.FetchModelsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	models, err := marketplaceapp.FetchUpstreamModels(req)
	respond(c, models, err)
}

func ListMyChannels(c *gin.Context) {
	result, err := marketplaceapp.ListOwnerChannels(c.GetInt("id"))
	respond(c, result, err)
}

func ListMyUsageLogs(c *gin.Context) {
	result, err := marketplaceapp.ListOwnerUsageLogs(c.GetInt("id"), marketplaceapp.OwnerUsageLogQuery{
		ChannelID: c.Query("channel_id"),
		Page:      queryInt(c, "page", 1),
		PageSize:  queryInt(c, "page_size", 20),
	})
	respond(c, result, err)
}

func UpdateChannel(c *gin.Context) {
	var req marketplaceapp.UpdateChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	result, err := marketplaceapp.UpdateOwnerChannel(c.GetInt("id"), c.Param("id"), req)
	respond(c, result, err)
}

func DeleteChannel(c *gin.Context) {
	respond(c, gin.H{"deleted": true}, marketplaceapp.DeleteOwnerChannel(c.GetInt("id"), c.Param("id")))
}

func VerifyChannel(c *gin.Context) {
	channels, err := marketplaceapp.ListOwnerChannels(c.GetInt("id"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	for _, channel := range channels {
		if channel.ID == c.Param("id") {
			if err := marketplaceapp.QueueNativeVerification(channel.ID); err != nil {
				httpapi.ApiError(c, err)
				return
			}
			httpapi.ApiSuccess(c, gin.H{"queued": true})
			return
		}
	}
	httpapi.ApiErrorMsg(c, "渠道不存在或无权限")
}

func PauseChannel(c *gin.Context) {
	respond(c, gin.H{"paused": true}, marketplaceapp.PauseOwnerChannel(c.GetInt("id"), c.Param("id"), true))
}

func ResumeChannel(c *gin.Context) {
	respond(c, gin.H{"resumed": true}, marketplaceapp.PauseOwnerChannel(c.GetInt("id"), c.Param("id"), false))
}

func BindToken(c *gin.Context) {
	var req marketplaceapp.TokenBindingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	err := marketplaceapp.BindTokenToMarketplaceGroup(c.GetInt("id"), req.TokenID, c.Param("id"))
	respond(c, gin.H{"token_id": req.TokenID, "group_id": c.Param("id")}, err)
}

func ListAdminChannels(c *gin.Context) {
	result, err := marketplaceapp.ListAdminChannels(c.Query("status"))
	respond(c, result, err)
}

func UpdateAdminChannel(c *gin.Context) {
	var req marketplaceapp.AdminUpdateChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	result, err := marketplaceapp.UpdateAdminChannel(c.Param("id"), req)
	respond(c, result, err)
}

func VerifyAdminChannel(c *gin.Context) {
	if err := marketplaceapp.QueueNativeVerification(c.Param("id")); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, gin.H{"queued": true})
}

func DeleteAdminChannel(c *gin.Context) {
	respond(c, gin.H{"deleted": true}, marketplaceapp.DeleteAdminChannel(c.Param("id")))
}

func ReviewChannel(c *gin.Context) {
	var req marketplaceapp.AdminReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	result, err := marketplaceapp.ReviewChannel(c.Param("id"), req)
	respond(c, result, err)
}

func respond(c *gin.Context, data any, err error) {
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, data)
}

func queryInt(c *gin.Context, name string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(c.Query(name)))
	if err != nil {
		return fallback
	}
	return value
}

func queryFloat(c *gin.Context, name string) float64 {
	value, _ := strconv.ParseFloat(strings.TrimSpace(c.Query(name)), 64)
	return value
}
