package http

import (
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	commercestore "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	"github.com/stripe/stripe-go/v87"
	"github.com/stripe/stripe-go/v87/webhook"
	waffocore "github.com/waffo-com/waffo-go/core"
	"io"
	stdhttp "net/http"
	"strings"
)

func RequestStripeAmount(c *gin.Context) {
	var req commerceapp.StripePayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(stdhttp.StatusOK, gin.H{"message": "error", "data": "鍙傛暟閿欒"})
		return
	}
	amount, err := commerceapp.QuoteStripeTopUpAmount(c.GetInt("id"), req)
	if err != nil {
		c.JSON(stdhttp.StatusOK, gin.H{"message": "error", "data": err.Error()})
		return
	}
	c.JSON(stdhttp.StatusOK, gin.H{"message": "success", "data": amount})
}

func RequestStripePay(c *gin.Context) {
	var req commerceapp.StripePayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(stdhttp.StatusOK, gin.H{"message": "error", "data": "鍙傛暟閿欒"})
		return
	}
	payload, err := commerceapp.CreateStripeTopUp(c.Request.Context(), c.GetInt("id"), req)
	if err != nil {
		c.JSON(stdhttp.StatusOK, gin.H{"message": "error", "data": err.Error()})
		return
	}
	c.JSON(stdhttp.StatusOK, gin.H{"message": "success", "data": payload})
}

func StripeWebhook(c *gin.Context) {
	ctx := c.Request.Context()
	if strings.TrimSpace(commercestore.StripeWebhookSecret) == "" {
		logger.LogWarn(ctx, "Stripe webhook 琚嫆缁?reason=webhook_disabled")
		c.AbortWithStatus(stdhttp.StatusForbidden)
		return
	}

	storage, err := platformhttpx.GetBodyStorage(c)
	if err != nil {
		c.AbortWithStatus(stdhttp.StatusServiceUnavailable)
		return
	}
	payload, err := storage.Bytes()
	if err != nil {
		c.AbortWithStatus(stdhttp.StatusServiceUnavailable)
		return
	}
	signature := c.GetHeader("Stripe-Signature")
	event, err := webhook.ConstructEventWithOptions(payload, signature, commercestore.StripeWebhookSecret, webhook.ConstructEventOptions{
		IgnoreAPIVersionMismatch: true,
	})
	if err != nil {
		c.AbortWithStatus(stdhttp.StatusBadRequest)
		return
	}

	var checkout stripe.CheckoutSession
	switch event.Type {
	case stripe.EventTypeCheckoutSessionCompleted, stripe.EventTypeCheckoutSessionAsyncPaymentSucceeded, stripe.EventTypeCheckoutSessionAsyncPaymentFailed, stripe.EventTypeCheckoutSessionExpired:
		if json.Unmarshal(event.Data.Raw, &checkout) != nil {
			c.AbortWithStatus(stdhttp.StatusBadRequest)
			return
		}
	default:
		c.Status(stdhttp.StatusOK)
		return
	}
	referenceID := checkout.ClientReferenceID
	if referenceID == "" {
		c.Status(stdhttp.StatusOK)
		return
	}
	customerID, intentID := "", ""
	if checkout.Customer != nil {
		customerID = checkout.Customer.ID
	}
	if checkout.PaymentIntent != nil {
		intentID = checkout.PaymentIntent.ID
	}
	evidence := map[string]any{
		"id": checkout.ID, "client_reference_id": referenceID, "mode": string(checkout.Mode),
		"status": string(checkout.Status), "payment_status": string(checkout.PaymentStatus),
		"payment_intent": intentID, "livemode": checkout.Livemode, "customer": customerID,
		"amount_total": checkout.AmountTotal, "currency": string(checkout.Currency), "event_type": string(event.Type),
	}
	switch event.Type {
	case stripe.EventTypeCheckoutSessionCompleted, stripe.EventTypeCheckoutSessionAsyncPaymentSucceeded:
		if checkout.Status == "complete" && checkout.PaymentStatus == "paid" {
			err = commerceapp.HandleStripeWebhookFulfillment(ctx, referenceID, customerID, evidence, c.ClientIP())
		}
	case stripe.EventTypeCheckoutSessionAsyncPaymentFailed:
		err = commerceapp.ExpireStripeOrder(ctx, referenceID, evidence)
	case stripe.EventTypeCheckoutSessionExpired:
		if checkout.Status == "expired" {
			err = commerceapp.ExpireStripeOrder(ctx, referenceID, evidence)
		}
	}
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Stripe webhook fulfillment failed event_id=%s trade_no=%s error=%q", event.ID, referenceID, err.Error()))
		c.AbortWithStatus(stdhttp.StatusServiceUnavailable)
		return
	}

	c.Status(stdhttp.StatusOK)
}

func RequestCreemPay(c *gin.Context) {
	var req commerceapp.CreemPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(stdhttp.StatusOK, gin.H{"message": "error", "data": "鍙傛暟閿欒"})
		return
	}
	payload, err := commerceapp.CreateCreemTopUp(c.Request.Context(), c.GetInt("id"), req)
	if err != nil {
		c.JSON(stdhttp.StatusOK, gin.H{"message": "error", "data": err.Error()})
		return
	}
	c.JSON(stdhttp.StatusOK, gin.H{"message": "success", "data": payload})
}

func CreemWebhook(c *gin.Context) {
	if !commerceapp.IsCreemWebhookEnabled() {
		c.AbortWithStatus(stdhttp.StatusForbidden)
		return
	}
	storage, err := platformhttpx.GetBodyStorage(c)
	if err != nil {
		c.AbortWithStatus(stdhttp.StatusBadRequest)
		return
	}
	body, err := storage.Bytes()
	if err != nil {
		c.AbortWithStatus(stdhttp.StatusBadRequest)
		return
	}
	signature := c.GetHeader(commerceapp.CreemSignatureHeader)
	if signature == "" || !commerceapp.VerifyCreemSignature(string(body), signature, commercestore.CreemWebhookSecret) {
		c.AbortWithStatus(stdhttp.StatusUnauthorized)
		return
	}

	var event commerceapp.CreemWebhookEvent
	if err := platformencoding.Unmarshal(body, &event); err != nil {
		c.AbortWithStatus(stdhttp.StatusBadRequest)
		return
	}
	if event.EventType == "checkout.completed" {
		if err := commerceapp.HandleCreemCheckoutCompleted(c.Request.Context(), &event, c.ClientIP()); err != nil {
			c.AbortWithStatus(stdhttp.StatusInternalServerError)
			return
		}
	}
	c.Status(stdhttp.StatusOK)
}

func RequestWaffoAmount(c *gin.Context) {
	var req commerceapp.WaffoPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(stdhttp.StatusOK, gin.H{"message": "error", "data": "鍙傛暟閿欒"})
		return
	}
	amount, err := commerceapp.QuoteWaffoTopUpAmount(c.GetInt("id"), req)
	if err != nil {
		c.JSON(stdhttp.StatusOK, gin.H{"message": "error", "data": err.Error()})
		return
	}
	c.JSON(stdhttp.StatusOK, gin.H{"message": "success", "data": amount})
}

func RequestWaffoPay(c *gin.Context) {
	var req commerceapp.WaffoPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(stdhttp.StatusOK, gin.H{"message": "error", "data": "鍙傛暟閿欒"})
		return
	}
	payload, err := commerceapp.CreateWaffoTopUp(c.Request.Context(), c.GetInt("id"), req)
	if err != nil {
		c.JSON(stdhttp.StatusOK, gin.H{"message": "error", "data": err.Error()})
		return
	}
	c.JSON(stdhttp.StatusOK, gin.H{"message": "success", "data": payload})
}

func WaffoWebhook(c *gin.Context) {
	if !commerceapp.IsWaffoWebhookEnabled() {
		c.AbortWithStatus(stdhttp.StatusForbidden)
		return
	}

	storage, err := platformhttpx.GetBodyStorage(c)
	if err != nil {
		c.AbortWithStatus(stdhttp.StatusBadRequest)
		return
	}
	body, err := storage.Bytes()
	if err != nil {
		c.AbortWithStatus(stdhttp.StatusBadRequest)
		return
	}
	sdk, err := commerceapp.GetWaffoSDK()
	if err != nil {
		c.AbortWithStatus(stdhttp.StatusInternalServerError)
		return
	}
	wh := sdk.Webhook()
	signature := c.GetHeader("X-SIGNATURE")
	if !wh.VerifySignature(string(body), signature) {
		c.AbortWithStatus(stdhttp.StatusBadRequest)
		return
	}

	var event waffocore.WebhookEvent
	if err := platformencoding.Unmarshal(body, &event); err != nil {
		respondWaffoWebhook(c, wh, false, "invalid payload")
		return
	}
	if event.EventType != waffocore.EventPayment {
		respondWaffoWebhook(c, wh, true, "")
		return
	}

	var payload struct {
		Result struct {
			MerchantOrderID string `json:"merchantOrderId"`
			OrderStatus     string `json:"orderStatus"`
		} `json:"result"`
	}
	if err := platformencoding.Unmarshal(body, &payload); err != nil {
		respondWaffoWebhook(c, wh, false, "invalid payment payload")
		return
	}
	if err := commerceapp.HandleWaffoPaymentStatus(c.Request.Context(), payload.Result.MerchantOrderID, payload.Result.OrderStatus, c.ClientIP()); err != nil {
		respondWaffoWebhook(c, wh, false, err.Error())
		return
	}
	respondWaffoWebhook(c, wh, true, "")
}

func RequestWaffoPancakeAmount(c *gin.Context) {
	var req commerceapp.WaffoPancakePayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(stdhttp.StatusOK, gin.H{"message": "error", "data": "鍙傛暟閿欒"})
		return
	}
	amount, err := commerceapp.QuoteWaffoPancakeTopUpAmount(c.GetInt("id"), req)
	if err != nil {
		c.JSON(stdhttp.StatusOK, gin.H{"message": "error", "data": err.Error()})
		return
	}
	c.JSON(stdhttp.StatusOK, gin.H{"message": "success", "data": amount})
}

func RequestWaffoPancakePay(c *gin.Context) {
	var req commerceapp.WaffoPancakePayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(stdhttp.StatusOK, gin.H{"message": "error", "data": "鍙傛暟閿欒"})
		return
	}
	payload, err := commerceapp.CreateWaffoPancakeTopUp(c.Request.Context(), c.GetInt("id"), req)
	if err != nil {
		c.JSON(stdhttp.StatusOK, gin.H{"message": "error", "data": err.Error()})
		return
	}
	c.JSON(stdhttp.StatusOK, gin.H{"message": "success", "data": payload})
}

func WaffoPancakeWebhook(c *gin.Context) {
	if !commerceapp.IsWaffoPancakeWebhookEnabled() {
		c.String(stdhttp.StatusForbidden, "webhook disabled")
		return
	}
	storage, err := platformhttpx.GetBodyStorage(c)
	if err != nil {
		c.String(stdhttp.StatusBadRequest, "bad request")
		return
	}
	body, err := storage.Bytes()
	if err != nil {
		c.String(stdhttp.StatusBadRequest, "bad request")
		return
	}
	event, err := commerceapp.VerifyConfiguredWaffoPancakeWebhook(string(body), c.GetHeader("X-Waffo-Signature"))
	if err != nil {
		c.String(stdhttp.StatusUnauthorized, "invalid signature")
		return
	}
	if event.NormalizedEventType() != "order.completed" {
		c.String(stdhttp.StatusOK, "OK")
		return
	}
	tradeNo, err := commerceapp.ResolveWaffoPancakeTradeNo(event)
	if err != nil {
		c.String(stdhttp.StatusOK, "OK")
		return
	}
	if err := commerceapp.CompleteWaffoPancakeTopUp(c.Request.Context(), tradeNo, event.ID, event.Data.OrderID, c.ClientIP()); err != nil {
		c.String(stdhttp.StatusInternalServerError, "retry")
		return
	}
	c.String(stdhttp.StatusOK, "OK")
}

func RequestXunhuPay(c *gin.Context) {
	req, ok := bindTopUpPaymentRequest(c)
	if !ok {
		return
	}
	respondXunhuTopUp(c, req)
}

func RequestNowPaymentsPay(c *gin.Context) {
	var req commerceapp.AmountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(stdhttp.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	payload, err := commerceapp.CreateNowPaymentsTopUp(c.Request.Context(), c.GetInt("id"), req)
	if err != nil {
		c.JSON(stdhttp.StatusOK, gin.H{"message": "error", "data": err.Error()})
		return
	}
	c.JSON(stdhttp.StatusOK, gin.H{"message": "success", "data": payload})
}

func NowPaymentsIPN(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil || len(body) == 0 {
		c.AbortWithStatus(stdhttp.StatusBadRequest)
		return
	}
	if err := commerceapp.HandleNowPaymentsIPN(c.Request.Context(), body, c.GetHeader(commerceapp.NowPaymentsSignatureHeader), c.ClientIP()); err != nil {
		if err.Error() == "invalid NOWPayments IPN" {
			c.AbortWithStatus(stdhttp.StatusUnauthorized)
			return
		}
		c.AbortWithStatus(stdhttp.StatusBadRequest)
		return
	}
	c.Status(stdhttp.StatusOK)
}

func respondXunhuTopUp(c *gin.Context, req commerceapp.EpayRequest) {
	payload, err := commerceapp.CreateXunhuTopUp(c.Request.Context(), c.GetInt("id"), req)
	if err != nil {
		c.JSON(stdhttp.StatusOK, gin.H{"message": "error", "data": err.Error()})
		return
	}
	c.JSON(stdhttp.StatusOK, gin.H{"message": "success", "data": payload})
}

func XunhuNotify(c *gin.Context) {
	if err := c.Request.ParseForm(); err != nil {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	params := map[string]string{}
	for key := range c.Request.Form {
		params[key] = c.Request.Form.Get(key)
	}
	ok, err := commerceapp.HandleXunhuTopUpWebhook(c.Request.Context(), params, c.ClientIP())
	if err != nil || !ok {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	_, _ = c.Writer.Write([]byte("success"))
}

func XunhuReturn(c *gin.Context) {
	c.Redirect(stdhttp.StatusFound, commerceapp.ResolveXunhuTopUpReturnURL(c.Query("trade_no")))
}

func respondWaffoWebhook(c *gin.Context, wh *waffocore.WebhookHandler, success bool, msg string) {
	var body, sig string
	if success {
		body, sig = wh.BuildSuccessResponse()
	} else {
		body, sig = wh.BuildFailedResponse(msg)
	}
	c.Header("X-SIGNATURE", sig)
	c.Data(stdhttp.StatusOK, "application/json", []byte(body))
}
