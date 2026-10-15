package http

import (
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/i18n"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
)

func RequestSubscriptionEpay(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}

	var req commerceapp.SubscriptionEpayPayRequest
	if err := platformhttpx.UnmarshalBodyReusable(c, &req); err != nil || req.PlanID <= 0 {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}

	if commerceapp.IsXunhuPaymentMethod(req.PaymentMethod) {
		payload, err := commerceapp.CreateSubscriptionXunhuPayment(c.GetInt("id"), commerceapp.SubscriptionXunhuPayRequest{
			PlanID:       req.PlanID,
			PurchaseType: req.PurchaseType,
			GroupBuyID:   req.GroupBuyID,
		})
		if err != nil {
			httpapi.ApiError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"message": "success",
			"data":    payload,
		})
		return
	}

	payload, err := commerceapp.CreateSubscriptionEpayPayment(c.GetInt("id"), req)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"form":       payload.Form,
			"order_id":   payload.OrderID,
			"amount_due": payload.AmountDue,
			"action":     payload.Action,
		},
		"url": payload.URL,
	})
}

func RequestSubscriptionXunhuPay(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}

	var req commerceapp.SubscriptionXunhuPayRequest
	if err := platformhttpx.UnmarshalBodyReusable(c, &req); err != nil || req.PlanID <= 0 {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}

	payload, err := commerceapp.CreateSubscriptionXunhuPayment(c.GetInt("id"), req)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data":    payload,
	})
}

func RequestSubscriptionStripePay(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}

	var req commerceapp.SubscriptionStripePayRequest
	if err := platformhttpx.UnmarshalBodyReusable(c, &req); err != nil || req.PlanID <= 0 {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}

	payload, err := commerceapp.CreateSubscriptionStripePayment(c.GetInt("id"), req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data":    payload,
	})
}

func RequestSubscriptionCreemPay(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}

	var req commerceapp.SubscriptionCreemPayRequest
	if err := platformhttpx.UnmarshalBodyReusable(c, &req); err != nil || req.PlanID <= 0 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "invalid request"})
		return
	}

	payload, err := commerceapp.CreateSubscriptionCreemPayment(c.GetInt("id"), req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data":    payload,
	})
}

func SubscriptionEpayNotify(c *gin.Context) {
	params, err := commerceapp.CollectEpayParams(c.Request)
	if err != nil || len(params) == 0 {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	verifyInfo, err := commerceapp.VerifySubscriptionEpay(params)
	if err != nil || verifyInfo.TradeStatus != "TRADE_SUCCESS" {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	if err := commerceapp.CompleteSubscriptionEpayPayment(verifyInfo); err != nil {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	_, _ = c.Writer.Write([]byte("success"))
}

func SubscriptionEpayReturn(c *gin.Context) {
	params, err := commerceapp.CollectEpayParams(c.Request)
	if err != nil || len(params) == 0 {
		c.Redirect(http.StatusFound, commerceapp.BuildPaymentReturnPath("/packages?pay=fail"))
		return
	}

	verifyInfo, err := commerceapp.VerifySubscriptionEpay(params)
	if err != nil {
		c.Redirect(http.StatusFound, commerceapp.BuildPaymentReturnPath("/packages?pay=fail"))
		return
	}
	c.Redirect(http.StatusFound, commerceapp.ResolveSubscriptionEpayReturnURL(verifyInfo))
}

func SubscriptionXunhuNotify(c *gin.Context) {
	if err := c.Request.ParseForm(); err != nil {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	params := make(map[string]string, len(c.Request.Form))
	for key := range c.Request.Form {
		params[key] = c.Request.Form.Get(key)
	}
	ok, err := commerceapp.CompleteSubscriptionXunhuPayment(params)
	if err != nil || !ok {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	_, _ = c.Writer.Write([]byte("success"))
}

func SubscriptionXunhuReturn(c *gin.Context) {
	c.Redirect(http.StatusFound, commerceapp.ResolveSubscriptionXunhuReturnURL(c.Query("trade_no")))
}

func PurchasePackage(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}

	var req commerceapp.SubscriptionPurchaseFields
	if err := platformhttpx.UnmarshalBodyReusable(c, &req); err != nil || req.PlanID <= 0 {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}

	switch strings.ToLower(strings.TrimSpace(req.PaymentMethod)) {
	case commerceschema.PaymentMethodStripe:
		RequestSubscriptionStripePay(c)
	case commerceschema.PaymentMethodCreem:
		RequestSubscriptionCreemPay(c)
	case commerceschema.PaymentMethodXunhu:
		RequestSubscriptionXunhuPay(c)
	default:
		RequestSubscriptionEpay(c)
	}
}

func UpgradePackage(c *gin.Context) {
	PurchasePackage(c)
}

func RenewPackage(c *gin.Context) {
	PurchasePackage(c)
}

func requirePaymentCompliance(c *gin.Context) bool {
	if !commerceapp.IsPaymentComplianceConfirmed() {
		httpapi.ApiErrorI18n(c, i18n.MsgPaymentComplianceRequired)
		return false
	}
	return true
}
