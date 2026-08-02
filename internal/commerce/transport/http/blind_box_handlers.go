package http

import (
	platformpagination "github.com/sh2001sh/new-api/internal/platform/pagination"
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
	stdhttp "net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
)

func getBlindBoxHistory(c *gin.Context) {
	pageInfo := platformpagination.GetPageQuery(c)
	payload, err := commerceapp.ListBlindBoxHistory(
		c.GetInt("id"),
		pageInfo.GetPage(),
		pageInfo.GetPageSize(),
	)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func getBlindBoxSelf(c *gin.Context) {
	payload, err := commerceapp.BuildBlindBoxSelfPayload(c.GetInt("id"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func adminGetBlindBoxUserOverview(c *gin.Context) {
	userID, err := strconv.Atoi(c.Param("id"))
	if err != nil || userID <= 0 {
		httpapi.ApiErrorMsg(c, "invalid user id")
		return
	}
	payload, err := commerceapp.BuildBlindBoxAdminOverviewPayload(userID)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func adminGrantBlindBoxes(c *gin.Context) {
	userID, err := strconv.Atoi(c.Param("id"))
	if err != nil || userID <= 0 {
		httpapi.ApiErrorMsg(c, "invalid user id")
		return
	}
	var req commerceapp.AdminBlindBoxGrantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}
	result, err := commerceapp.GrantBlindBoxes(userID, c.GetInt("id"), req)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, gin.H{
		"grant": result.Grant,
		"order": result.Order,
	})
}

func useBlindBoxProp(c *gin.Context) {
	propID, err := strconv.Atoi(c.Param("id"))
	if err != nil || propID <= 0 {
		httpapi.ApiErrorMsg(c, "invalid blind box prop id")
		return
	}
	prop, err := commerceapp.ActivateBlindBoxProp(c.GetInt("id"), propID)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, gin.H{"prop": prop})
}

func getBlindBoxOrderStatus(c *gin.Context) {
	payload, err := commerceapp.BuildBlindBoxOrderStatusPayload(c.GetInt("id"), c.Param("trade_no"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func requestBlindBoxAmount(c *gin.Context) {
	var req commerceapp.BlindBoxAmountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}
	amount, err := commerceapp.QuoteBlindBoxPurchase(c.GetInt("id"), req.Quantity)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, amount)
}

func requestBlindBoxPay(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}
	var req commerceapp.BlindBoxPayRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Quantity <= 0 {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}
	if commerceapp.IsXunhuPaymentMethod(req.PaymentMethod) {
		payload, err := commerceapp.CreateBlindBoxXunhuPayment(c.GetInt("id"), req.Quantity)
		if err != nil {
			httpapi.ApiError(c, err)
			return
		}
		c.JSON(stdhttp.StatusOK, gin.H{
			"message": "success",
			"data":    payload,
		})
		return
	}
	payload, err := commerceapp.CreateBlindBoxEpayPayment(c.GetInt("id"), req)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"form":       payload.Form,
			"order_id":   payload.OrderID,
			"amount_due": payload.AmountDue,
			"quantity":   payload.Quantity,
		},
		"url": payload.URL,
	})
}

func openBlindBox(c *gin.Context) {
	var req commerceapp.BlindBoxOpenRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Count <= 0 {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}
	payload, err := commerceapp.BuildBlindBoxOpenPayload(c.GetInt("id"), req.Count)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func blindBoxEpayNotify(c *gin.Context) {
	params, err := commerceapp.CollectEpayParams(c.Request)
	if err != nil || len(params) == 0 {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	verifyInfo, err := commerceapp.VerifyBlindBoxEpay(params)
	if err != nil || verifyInfo.TradeStatus != "TRADE_SUCCESS" {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	if err := commerceapp.CompleteBlindBoxEpayPayment(verifyInfo); err != nil {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	_, _ = c.Writer.Write([]byte("success"))
}

func blindBoxEpayReturn(c *gin.Context) {
	params, err := commerceapp.CollectEpayParams(c.Request)
	if err != nil || len(params) == 0 {
		c.Redirect(stdhttp.StatusFound, commerceapp.BuildPaymentReturnPath("/blind-box?pay=fail"))
		return
	}
	verifyInfo, err := commerceapp.VerifyBlindBoxEpay(params)
	if err != nil {
		c.Redirect(stdhttp.StatusFound, commerceapp.BuildPaymentReturnPath("/blind-box?pay=fail"))
		return
	}
	c.Redirect(stdhttp.StatusFound, commerceapp.ResolveBlindBoxEpayReturnURL(verifyInfo))
}

func blindBoxXunhuNotify(c *gin.Context) {
	if err := c.Request.ParseForm(); err != nil {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	params := make(map[string]string, len(c.Request.Form))
	for key := range c.Request.Form {
		params[key] = c.Request.Form.Get(key)
	}
	ok, err := commerceapp.CompleteBlindBoxXunhuPayment(params)
	if err != nil || !ok {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	_, _ = c.Writer.Write([]byte("success"))
}

func blindBoxXunhuReturn(c *gin.Context) {
	c.Redirect(stdhttp.StatusFound, commerceapp.ResolveBlindBoxXunhuReturnURL(c.Query("trade_no")))
}
