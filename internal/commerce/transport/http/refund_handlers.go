package http

import (
	"strings"

	"github.com/gin-gonic/gin"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
)

func listRefundableOrders(c *gin.Context) {
	orders, err := commerceapp.ListRefundableOrders(c.GetInt("id"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, gin.H{"items": orders})
}

func createUserRefund(c *gin.Context) {
	var req commerceschema.RefundRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiErrorMsg(c, "退款订单参数无效")
		return
	}
	result, err := commerceapp.CreateUserRefund(c.GetInt("id"), req)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, result)
}

func syncUserRefund(c *gin.Context) {
	refundNo := strings.TrimSpace(c.Param("refund_no"))
	if refundNo == "" {
		httpapi.ApiErrorMsg(c, "退款单号不能为空")
		return
	}
	result, err := commerceapp.SyncUserRefund(c.GetInt("id"), refundNo)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, result)
}
