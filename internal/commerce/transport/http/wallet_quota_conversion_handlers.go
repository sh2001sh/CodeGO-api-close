package http

import (
	"github.com/gin-gonic/gin"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
)

func getWalletQuotaConversions(c *gin.Context) {
	payload, err := commerceapp.BuildWalletQuotaConversionOverview(c.GetInt("id"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func createWalletQuotaConversion(c *gin.Context) {
	var req commerceapp.CreateWalletQuotaConversionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiErrorMsg(c, "额度转换参数无效")
		return
	}
	conversion, err := commerceapp.ConvertWalletQuota(c.GetInt("id"), req)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, conversion)
}
