package http

import (
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/i18n"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	platformpagination "github.com/sh2001sh/new-api/internal/platform/pagination"
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
	stdhttp "net/http"
)

func GetTopUpInfo(c *gin.Context) {
	httpapi.ApiSuccess(c, commerceapp.BuildTopUpInfo(c.GetInt("id")))
}

func RedeemTopUpCode(c *gin.Context) {
	var req commerceapp.RedemptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, err)
		return
	}

	result, err := commerceapp.RedeemTopUpCode(c.GetInt("id"), req.Key)
	if err != nil {
		switch {
		case errors.Is(err, commerceapp.ErrPaymentComplianceRequired):
			httpapi.ApiErrorI18n(c, i18n.MsgPaymentComplianceRequired)
		case errors.Is(err, commerceapp.ErrTopUpProcessing):
			httpapi.ApiErrorI18n(c, i18n.MsgUserTopUpProcessing)
		case errors.Is(err, commercedomain.ErrRedemptionInvalid):
			httpapi.ApiErrorI18n(c, i18n.MsgRedemptionInvalid)
		case errors.Is(err, commercedomain.ErrRedemptionUsed):
			httpapi.ApiErrorI18n(c, i18n.MsgRedemptionUsed)
		case errors.Is(err, commercedomain.ErrRedemptionExpired):
			httpapi.ApiErrorI18n(c, i18n.MsgRedemptionExpired)
		case errors.Is(err, commercedomain.ErrRedemptionBusy):
			httpapi.ApiErrorI18n(c, i18n.MsgRedemptionBusy)
		default:
			httpapi.ApiErrorI18n(c, i18n.MsgRedemptionBusy)
		}
		return
	}

	c.JSON(stdhttp.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    result,
	})
}

func RequestEpay(c *gin.Context) {
	req, ok := bindTopUpPaymentRequest(c)
	if !ok {
		return
	}
	respondEpayTopUp(c, req)
}

// RequestTopUpPayment only routes the explicit xunhu type to XunhuPay.
// Generic wxpay remains available to Epay-compatible gateways such as JianPay.
func RequestTopUpPayment(c *gin.Context) {
	req, ok := bindTopUpPaymentRequest(c)
	if !ok {
		return
	}
	if commerceapp.IsXunhuPaymentMethod(req.PaymentMethod) {
		respondXunhuTopUp(c, req)
		return
	}
	respondEpayTopUp(c, req)
}

func bindTopUpPaymentRequest(c *gin.Context) (commerceapp.EpayRequest, bool) {
	var req commerceapp.EpayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(stdhttp.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return commerceapp.EpayRequest{}, false
	}
	return req, true
}

func respondEpayTopUp(c *gin.Context, req commerceapp.EpayRequest) {
	response, err := commerceapp.CreateEpayTopUp(c.GetInt("id"), req)
	if err != nil {
		c.JSON(stdhttp.StatusOK, gin.H{"message": "error", "data": err.Error()})
		return
	}
	c.JSON(stdhttp.StatusOK, gin.H{"message": "success", "data": response.Data, "url": response.URL})
}

func EpayNotify(c *gin.Context) {
	params, err := commerceapp.CollectEpayParams(c.Request)
	if err != nil {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	ok, err := commerceapp.HandleEpayWebhook(params, c.ClientIP())
	if err != nil || !ok {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	_, _ = c.Writer.Write([]byte("success"))
}

func RequestAmount(c *gin.Context) {
	var req commerceapp.AmountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(stdhttp.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	amount, err := commerceapp.QuoteTopUpAmount(c.GetInt("id"), req)
	if err != nil {
		c.JSON(stdhttp.StatusOK, gin.H{"message": "error", "data": err.Error()})
		return
	}
	c.JSON(stdhttp.StatusOK, gin.H{"message": "success", "data": amount})
}

func RequestNowPaymentsAmount(c *gin.Context) {
	var req commerceapp.AmountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(stdhttp.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	amount, err := commerceapp.QuoteNowPaymentsTopUpAmount(c.GetInt("id"), req)
	if err != nil {
		c.JSON(stdhttp.StatusOK, gin.H{"message": "error", "data": err.Error()})
		return
	}
	c.JSON(stdhttp.StatusOK, gin.H{"message": "success", "data": amount})
}

func GetUserTopUps(c *gin.Context) {
	pageInfo, err := commerceapp.ListUserTopUps(c.GetInt("id"), c.Query("keyword"), platformpagination.GetPageQuery(c))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, pageInfo)
}

func GetAllTopUps(c *gin.Context) {
	pageInfo, err := commerceapp.ListAllTopUps(c.Query("keyword"), platformpagination.GetPageQuery(c))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, pageInfo)
}

func AdminCompleteTopUp(c *gin.Context) {
	var req commerceapp.AdminCompleteTopupRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.TradeNo == "" {
		httpapi.ApiErrorMsg(c, "参数错误")
		return
	}
	if err := commerceapp.CompleteTopUpByAdmin(req.TradeNo, c.ClientIP()); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, nil)
}
