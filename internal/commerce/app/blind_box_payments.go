package app

import (
	"errors"
	"fmt"
	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/sh2001sh/new-api/constant"
	commercestore "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	platformtext "github.com/sh2001sh/new-api/internal/platform/textx"
	"net/url"
	"strconv"
	"time"
)

type BlindBoxEpayCheckoutPayload struct {
	OrderID   string            `json:"order_id"`
	AmountDue float64           `json:"amount_due"`
	Quantity  int               `json:"quantity"`
	Form      map[string]string `json:"form"`
	URL       string            `json:"-"`
}

type BlindBoxXunhuCheckoutPayload struct {
	OrderID   string  `json:"order_id"`
	AmountDue float64 `json:"amount_due"`
	Quantity  int     `json:"quantity"`
	PayURL    string  `json:"pay_url"`
	QRCodeURL string  `json:"qrcode_url"`
}

// CreateBlindBoxEpayPayment creates a blind-box epay checkout payload.
func CreateBlindBoxEpayPayment(userID int, req BlindBoxPayRequest) (*BlindBoxEpayCheckoutPayload, error) {
	if !commercestore.ContainsPayMethod(req.PaymentMethod) {
		return nil, errors.New("payment method is not available")
	}
	amountDue, err := ValidateBlindBoxPurchase(userID, req.Quantity)
	if err != nil {
		return nil, err
	}
	callBackAddress := CallbackAddress()
	returnURL, err := url.Parse(blindBoxPendingReturnURL())
	if err != nil {
		return nil, errors.New("payment callback address is invalid")
	}
	notifyURL, err := url.Parse(callBackAddress + "/api/blind-box/epay/notify")
	if err != nil {
		return nil, errors.New("payment callback address is invalid")
	}
	tradeNo := fmt.Sprintf("BBUSR%dNO%s%d", userID, platformruntime.GetRandomString(6), time.Now().Unix())
	client := GetEpayClient()
	if client == nil {
		return nil, errors.New("payment gateway is not configured")
	}
	order := &commerceschema.BlindBoxOrder{
		UserId:          userID,
		Quantity:        req.Quantity,
		Money:           amountDue,
		TradeNo:         tradeNo,
		PaymentMethod:   req.PaymentMethod,
		PaymentProvider: commerceschema.PaymentProviderEpay,
		Source:          commerceschema.BlindBoxOrderSourcePurchase,
		CreateTime:      time.Now().Unix(),
		Status:          constant.TopUpStatusPending,
	}
	if err := CreatePendingBlindBoxOrder(order); err != nil {
		return nil, errors.New("failed to create order")
	}
	uri, params, err := client.Purchase(&epay.PurchaseArgs{
		Type:           req.PaymentMethod,
		ServiceTradeNo: tradeNo,
		Name:           fmt.Sprintf("BlindBox:%d", req.Quantity),
		Money:          strconv.FormatFloat(amountDue, 'f', 2, 64),
		Device:         epay.PC,
		NotifyUrl:      notifyURL,
		ReturnUrl:      returnURL,
	})
	if err != nil {
		_ = ExpireBlindBoxOrder(tradeNo, commerceschema.PaymentProviderEpay)
		return nil, errors.New("failed to create payment")
	}
	return &BlindBoxEpayCheckoutPayload{
		OrderID:   tradeNo,
		AmountDue: amountDue,
		Quantity:  req.Quantity,
		Form:      params,
		URL:       uri,
	}, nil
}

// CreateBlindBoxXunhuPayment creates a blind-box xunhu checkout payload.
func CreateBlindBoxXunhuPayment(userID int, quantity int) (*BlindBoxXunhuCheckoutPayload, error) {
	if !IsXunhuTopUpEnabled() {
		return nil, errors.New("payment method is not available")
	}
	amountDue, err := ValidateBlindBoxPurchase(userID, quantity)
	if err != nil {
		return nil, err
	}
	callbackAddress := CallbackAddress()
	tradeNo := fmt.Sprintf("BBUSR%dNO%s%d", userID, platformruntime.GetRandomString(6), time.Now().Unix())
	notifyURL := callbackAddress + "/api/blind-box/xunhu/notify"
	returnURL := callbackAddress + "/api/blind-box/xunhu/return?trade_no=" + tradeNo
	order := &commerceschema.BlindBoxOrder{
		UserId:          userID,
		Quantity:        quantity,
		Money:           amountDue,
		TradeNo:         tradeNo,
		PaymentMethod:   commerceschema.PaymentMethodXunhu,
		PaymentProvider: commerceschema.PaymentProviderXunhu,
		Source:          commerceschema.BlindBoxOrderSourcePurchase,
		CreateTime:      time.Now().Unix(),
		Status:          constant.TopUpStatusPending,
	}
	if err := CreatePendingBlindBoxOrder(order); err != nil {
		return nil, errors.New("failed to create order")
	}
	payResult, err := CreateXunhuOrder(tradeNo, fmt.Sprintf("BlindBox:%d", quantity), amountDue, notifyURL, returnURL)
	if err != nil {
		_ = ExpireBlindBoxOrder(tradeNo, commerceschema.PaymentProviderXunhu)
		return nil, errors.New(FormatXunhuCreatePaymentError(err))
	}
	return &BlindBoxXunhuCheckoutPayload{
		OrderID:   tradeNo,
		AmountDue: amountDue,
		Quantity:  quantity,
		PayURL:    payResult.PayURL,
		QRCodeURL: payResult.QRCodeURL,
	}, nil
}

// VerifyBlindBoxEpay verifies blind-box epay callback parameters.
func VerifyBlindBoxEpay(params map[string]string) (*epay.VerifyRes, error) {
	client := GetEpayClient()
	if client == nil {
		return nil, errors.New("payment gateway is not configured")
	}
	verifyInfo, err := client.Verify(params)
	if err != nil || !verifyInfo.VerifyStatus {
		return nil, errors.New("verify failed")
	}
	return verifyInfo, nil
}

// CompleteBlindBoxEpayPayment completes a paid blind-box epay order.
func CompleteBlindBoxEpayPayment(verifyInfo *epay.VerifyRes) error {
	if verifyInfo == nil || verifyInfo.TradeStatus != epay.StatusTradeSuccess {
		return errors.New("payment not complete")
	}
	return completeBlindBoxOrder(verifyInfo.ServiceTradeNo, platformtext.GetJsonString(verifyInfo), commerceschema.PaymentProviderEpay, verifyInfo.Type)
}

// ResolveBlindBoxEpayReturnURL resolves the browser return target for an epay blind-box order.
func ResolveBlindBoxEpayReturnURL(verifyInfo *epay.VerifyRes) string {
	if verifyInfo == nil {
		return blindBoxFailedReturnURL()
	}
	if verifyInfo.TradeStatus == epay.StatusTradeSuccess {
		if err := CompleteBlindBoxEpayPayment(verifyInfo); err == nil {
			return blindBoxSuccessReturnURL()
		}
		return blindBoxFailedReturnURL()
	}
	return blindBoxPendingReturnURL()
}

// CompleteBlindBoxXunhuPayment completes a paid blind-box xunhu order from webhook params.
func CompleteBlindBoxXunhuPayment(params map[string]string) (bool, error) {
	if !IsXunhuWebhookEnabled() {
		return false, nil
	}
	if !VerifyXunhuHash(params) {
		return false, nil
	}
	if params["status"] != "OD" {
		return true, nil
	}
	tradeNo := params["trade_order_id"]
	if err := completeBlindBoxOrder(tradeNo, platformtext.GetJsonString(params), commerceschema.PaymentProviderXunhu, commerceschema.PaymentMethodXunhu); err != nil {
		return false, err
	}
	return true, nil
}

// ResolveBlindBoxXunhuReturnURL resolves the browser return target for a xunhu blind-box order.
func ResolveBlindBoxXunhuReturnURL(tradeNo string) string {
	return resolveBlindBoxXunhuReturnURL(tradeNo)
}
