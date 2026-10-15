package http

import (
	"errors"
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
	"strconv"

	"github.com/gin-gonic/gin"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
)

func listGroupBuys(c *gin.Context) {
	payload, err := commerceapp.BuildActiveGroupBuysPayload(c.GetInt("id"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func listMyGroupBuys(c *gin.Context) {
	payload, err := commerceapp.BuildUserGroupBuysPayload(c.GetInt("id"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func getGroupBuy(c *gin.Context) {
	groupBuyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || groupBuyID <= 0 {
		httpapi.ApiErrorMsg(c, "invalid group buy id")
		return
	}
	item, err := commerceapp.GetGroupBuyDetail(c.GetInt("id"), groupBuyID)
	if err != nil {
		if errors.Is(err, commerceapp.ErrGroupBuyNotFound) {
			httpapi.ApiErrorMsg(c, "group buy not found")
			return
		}
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, item)
}

func joinGroupBuy(c *gin.Context) {
	var req commerceapp.GroupBuyJoinRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	if req.GroupBuyId <= 0 {
		httpapi.ApiErrorMsg(c, "invalid group buy id")
		return
	}
	payload, err := commerceapp.JoinGroupBuy(c.GetInt("id"), req)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}
