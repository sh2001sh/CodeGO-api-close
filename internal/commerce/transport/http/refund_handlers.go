package http

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
)

func refundTargetUserID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"success": false, "message": "退款用户参数无效"})
		return 0, false
	}
	return id, true
}

func refundAPIError(c *gin.Context, err error) {
	if errors.Is(err, commerceapp.ErrRefundAdminRequired) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "message": err.Error()})
		return
	}
	httpapi.ApiError(c, err)
}

func listAdminRefundableOrders(c *gin.Context) {
	userID, ok := refundTargetUserID(c)
	if !ok {
		return
	}
	orders, err := commerceapp.ListAdminRefundableOrders(c.GetInt("id"), userID)
	if err != nil {
		refundAPIError(c, err)
		return
	}
	httpapi.ApiSuccess(c, gin.H{"items": orders})
}

func createAdminRefund(c *gin.Context) {
	userID, ok := refundTargetUserID(c)
	if !ok {
		return
	}
	var req commerceschema.RefundRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiErrorMsg(c, "退款订单参数无效")
		return
	}
	result, err := commerceapp.CreateAdminRefund(c.GetInt("id"), userID, req)
	if err != nil {
		refundAPIError(c, err)
		return
	}
	httpapi.ApiSuccess(c, result)
}

func syncAdminRefund(c *gin.Context) {
	userID, ok := refundTargetUserID(c)
	if !ok {
		return
	}
	refundNo := strings.TrimSpace(c.Param("refund_no"))
	if refundNo == "" {
		httpapi.ApiErrorMsg(c, "退款单号不能为空")
		return
	}
	result, err := commerceapp.SyncAdminRefund(c.GetInt("id"), userID, refundNo)
	if err != nil {
		refundAPIError(c, err)
		return
	}
	httpapi.ApiSuccess(c, result)
}
