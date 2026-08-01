package http

import (
	"strconv"

	"github.com/gin-gonic/gin"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	platformpagination "github.com/sh2001sh/new-api/internal/platform/pagination"
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
)

func getDailyLuckyNumberSelf(c *gin.Context) {
	payload, err := commerceapp.BuildDailyLuckyNumberSelfPayload(c.GetInt("id"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func getDailyLuckyNumberHistory(c *gin.Context) {
	page := platformpagination.GetPageQuery(c)
	payload, err := commerceapp.ListDailyLuckyNumberHistory(c.GetInt("id"), page.GetPage(), page.GetPageSize())
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func getDailyLuckyNumberPublicWins(c *gin.Context) {
	page := platformpagination.GetPageQuery(c)
	payload, err := commerceapp.ListDailyLuckyNumberPublicWins(page.GetPage(), page.GetPageSize())
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func getAdminDailyLuckyNumberConfig(c *gin.Context) {
	httpapi.ApiSuccess(c, commerceapp.GetDailyLuckyNumberConfig())
}

func updateAdminDailyLuckyNumberConfig(c *gin.Context) {
	var req commerceapp.LuckyNumberConfigUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}
	config, err := commerceapp.UpdateDailyLuckyNumberConfig(req)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, config)
}

func listAdminDailyLuckyNumberDraws(c *gin.Context) {
	page := platformpagination.GetPageQuery(c)
	payload, err := commerceapp.BuildDailyLuckyNumberAdminPayload(page.GetPage(), page.GetPageSize())
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func retryAdminDailyLuckyNumberDraw(c *gin.Context) {
	drawID, err := strconv.Atoi(c.Param("id"))
	if err != nil || drawID <= 0 {
		httpapi.ApiErrorMsg(c, "invalid draw id")
		return
	}
	if err := commerceapp.RetryDailyLuckyDraw(drawID); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, nil)
}

func backfillAdminDailyLuckyNumbers(c *gin.Context) {
	result, err := commerceapp.BackfillDailyLuckyNumbers()
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, result)
}
