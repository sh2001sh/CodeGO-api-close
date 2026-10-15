package http

import (
	platformpagination "github.com/sh2001sh/new-api/internal/platform/pagination"
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
	stdhttp "net/http"
	"strconv"
	"strings"

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

func adminRevokeBlindBoxes(c *gin.Context) {
	userID, err := strconv.Atoi(c.Param("id"))
	if err != nil || userID <= 0 {
		httpapi.ApiErrorMsg(c, "invalid user id")
		return
	}
	var req struct {
		Quantity int    `json:"quantity"`
		Reason   string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}
	count, err := commerceapp.RevokeBlindBoxes(userID, c.GetInt("id"), req.Quantity, req.Reason)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, gin.H{"revoked": count})
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

func pauseBlindBoxProp(c *gin.Context) {
	propID, err := strconv.Atoi(c.Param("id"))
	if err != nil || propID <= 0 {
		httpapi.ApiErrorMsg(c, "invalid blind box prop id")
		return
	}
	prop, err := commerceapp.PauseBlindBoxProp(c.GetInt("id"), propID)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, gin.H{"prop": prop})
}

func convertBlindBoxProp(c *gin.Context) {
	propID, err := strconv.Atoi(c.Param("id"))
	if err != nil || propID <= 0 {
		httpapi.ApiErrorMsg(c, "invalid blind box prop id")
		return
	}
	var req struct {
		TargetType string `json:"target_type" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}
	prop, err := commerceapp.ConvertBlindBoxDiscountProp(c.GetInt("id"), propID, req.TargetType)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, gin.H{"prop": prop})
}

func giftBlindBoxProp(c *gin.Context) {
	propID, err := strconv.Atoi(c.Param("id"))
	if err != nil || propID <= 0 {
		httpapi.ApiErrorMsg(c, "invalid blind box prop id")
		return
	}
	var req commerceapp.GiftBlindBoxPropRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiErrorMsg(c, "recipient_external_id and request_id are required")
		return
	}
	result, err := commerceapp.GiftBlindBoxProp(c.GetInt("id"), propID, req)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, result)
}

func getBalanceBlindBoxOverview(c *gin.Context) {
	overview, err := commerceapp.GetBalanceBlindBoxOverview(c.GetInt("id"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, gin.H{"inventory": overview})
}

func purchaseBalanceBlindBoxes(c *gin.Context) {
	var req struct {
		RequestID string `json:"request_id" binding:"required"`
		Count     int    `json:"count" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiErrorMsg(c, "request_id and count are required")
		return
	}
	result, err := commerceapp.PurchaseBalanceBlindBoxes(c.GetInt("id"), req.RequestID, req.Count)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, result)
}

func openBalanceBlindBox(c *gin.Context) {
	var req struct {
		RequestID string `json:"request_id" binding:"required"`
		Count     int    `json:"count"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiErrorMsg(c, "request_id is required")
		return
	}
	result, err := commerceapp.OpenBalanceBlindBox(c.GetInt("id"), req.RequestID, req.Count)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, result)
}

func simulateBalanceBlindBoxes(c *gin.Context) {
	var req struct {
		BalanceQuota      int64 `json:"balance_quota" binding:"required"`
		Count             int   `json:"count" binding:"required"`
		SmallPityProgress int   `json:"small_pity_progress"`
		PityProgress      int   `json:"pity_progress"`
		FirstDrawEligible *bool `json:"first_draw_eligible"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiErrorMsg(c, "balance_quota and count are required")
		return
	}
	firstDrawEligible := true
	if req.FirstDrawEligible != nil {
		firstDrawEligible = *req.FirstDrawEligible
	}
	result, err := commerceapp.SimulateBalanceBlindBoxes(req.BalanceQuota, req.Count, commerceapp.BalanceBlindBoxSimulationState{
		SmallPityProgress: req.SmallPityProgress, PityProgress: req.PityProgress,
		FirstDrawEligible: firstDrawEligible,
	})
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, result)
}

func giftBalanceBlindBoxes(c *gin.Context) {
	var req commerceapp.GiftBalanceBlindBoxRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiErrorMsg(c, "recipient_external_id, request_id and count are required")
		return
	}
	result, err := commerceapp.GiftBalanceBlindBoxes(c.GetInt("id"), req)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, result)
}

func getBlindBoxOrderStatus(c *gin.Context) {
	payload, err := commerceapp.BuildBlindBoxOrderStatusPayload(c.GetInt("id"), c.Param("trade_no"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func cancelBlindBoxOrder(c *gin.Context) {
	tradeNo := strings.TrimSpace(c.Param("trade_no"))
	if tradeNo == "" {
		httpapi.ApiErrorMsg(c, "invalid trade no")
		return
	}
	if err := commerceapp.CancelPendingBlindBoxOrder(c.GetInt("id"), tradeNo); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, nil)
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
