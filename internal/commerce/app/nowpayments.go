package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/constant"
	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	commercestore "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformgeneral "github.com/sh2001sh/new-api/internal/platform/general"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/shopspring/decimal"
)

const (
	PaymentMethodNowPayments   = "nowpayments"
	PaymentProviderNowPayments = "nowpayments"
	nowPaymentsAPIBaseURL      = "https://api.nowpayments.io/v1"
	NowPaymentsSignatureHeader = "x-nowpayments-sig"
)

type NowPaymentsCheckoutPayload struct {
	PayURL                 string `json:"pay_url"`
	OrderID                string `json:"order_id"`
	PaymentID              string `json:"payment_id"`
	PayAddress             string `json:"pay_address,omitempty"`
	PayAmount              string `json:"pay_amount,omitempty"`
	PayCurrency            string `json:"pay_currency,omitempty"`
	ExpirationEstimateDate string `json:"expiration_estimate_date,omitempty"`
}

type nowPaymentsPaymentRequest struct {
	PriceAmount      float64 `json:"price_amount"`
	PriceCurrency    string  `json:"price_currency"`
	PayCurrency      string  `json:"pay_currency"`
	OrderID          string  `json:"order_id"`
	OrderDescription string  `json:"order_description"`
	IPNCallbackURL   string  `json:"ipn_callback_url"`
	SuccessURL       string  `json:"success_url"`
	CancelURL        string  `json:"cancel_url"`
}

type nowPaymentsPaymentResponse struct {
	PaymentID              json.RawMessage `json:"payment_id"`
	PayAddress             string          `json:"pay_address"`
	PayAmount              json.RawMessage `json:"pay_amount"`
	PayCurrency            string          `json:"pay_currency"`
	InvoiceURL             string          `json:"invoice_url"`
	ExpirationEstimateDate string          `json:"expiration_estimate_date"`
}

type NowPaymentsIPNPayload struct {
	PaymentStatus string          `json:"payment_status"`
	OrderID       string          `json:"order_id"`
	PaymentID     json.RawMessage `json:"payment_id"`
	ActuallyPaid  json.RawMessage `json:"actually_paid"`
}

func IsNowPaymentsTopUpEnabled() bool {
	if !IsPaymentComplianceConfirmed() || !commercestore.NowPaymentsEnabled {
		return false
	}
	return strings.TrimSpace(commercestore.NowPaymentsApiKey) != "" &&
		strings.TrimSpace(commercestore.NowPaymentsIPNSecret) != "" &&
		commercestore.NowPaymentsQuotaPerUSDT > 0 &&
		commercestore.NowPaymentsMinTopUp > 0 &&
		strings.EqualFold(strings.TrimSpace(commercestore.NowPaymentsPaymentCurrency), "usdt") &&
		strings.TrimSpace(commercestore.NowPaymentsPayCurrency) != ""
}

func BuildNowPaymentsPayMethod() map[string]string {
	return map[string]string{
		"name":      "USDT（NOWPayments）",
		"type":      PaymentMethodNowPayments,
		"color":     "rgba(var(--semi-green-5), 1)",
		"min_topup": fmt.Sprintf("%d", GetNowPaymentsMinTopup()),
		"currency":  "USDT",
	}
}

func GetNowPaymentsMinTopup() int64 {
	minTopUp := int64(commercestore.NowPaymentsMinTopUp)
	if platformgeneral.GetQuotaDisplayType() == platformgeneral.QuotaDisplayTypeTokens {
		minTopUp = decimal.NewFromInt(minTopUp).
			Mul(decimal.NewFromFloat(platformruntime.QuotaPerUnit)).
			IntPart()
	}
	return minTopUp
}

func GetNowPaymentsUSDTAmount(amount int64) float64 {
	if amount <= 0 || commercestore.NowPaymentsQuotaPerUSDT <= 0 {
		return 0
	}
	quotaAmount := decimal.NewFromInt(amount)
	if platformgeneral.GetQuotaDisplayType() == platformgeneral.QuotaDisplayTypeTokens {
		if platformruntime.QuotaPerUnit <= 0 {
			return 0
		}
		quotaAmount = quotaAmount.Div(decimal.NewFromFloat(platformruntime.QuotaPerUnit))
	}
	return quotaAmount.
		Div(decimal.NewFromFloat(commercestore.NowPaymentsQuotaPerUSDT)).
		Round(6).InexactFloat64()
}

func QuoteNowPaymentsTopUpAmount(userID int, req AmountRequest) (string, error) {
	if !IsNowPaymentsTopUpEnabled() {
		return "", errors.New("NOWPayments is not configured")
	}
	if req.Amount < GetNowPaymentsMinTopup() {
		return "", fmt.Errorf("充值数量不能小于 %d", GetNowPaymentsMinTopup())
	}
	amount := GetNowPaymentsUSDTAmount(req.Amount)
	amount = ApplyTopupBlindBoxDiscount(userID, amount)
	if amount <= 0 {
		return "", errors.New("充值金额过低")
	}
	return fmt.Sprintf("%.6f", amount), nil
}

func CreateNowPaymentsTopUp(ctx context.Context, userID int, req AmountRequest) (*NowPaymentsCheckoutPayload, error) {
	if !IsNowPaymentsTopUpEnabled() {
		return nil, errors.New("NOWPayments is not configured")
	}
	if req.Amount < GetNowPaymentsMinTopup() {
		return nil, fmt.Errorf("充值数量不能小于 %d", GetNowPaymentsMinTopup())
	}
	baseAmount := GetNowPaymentsUSDTAmount(req.Amount)
	if baseAmount <= 0 {
		return nil, errors.New("充值金额过低")
	}
	tradeNo := fmt.Sprintf("USR%dNO%s%d", userID, platformruntime.GetRandomString(8), time.Now().Unix())
	amount := NormalizeStoredTopupAmount(req.Amount, NormalizeTopupWalletType(req.WalletType))
	topup := &commerceschema.TopUp{
		UserId:          userID,
		Amount:          amount,
		Money:           baseAmount,
		TradeNo:         tradeNo,
		PaymentMethod:   PaymentMethodNowPayments,
		PaymentProvider: PaymentProviderNowPayments,
		WalletType:      NormalizeTopupWalletType(req.WalletType),
		CreateTime:      time.Now().Unix(),
		Status:          constant.TopUpStatusPending,
	}
	if _, err := CreatePendingTopUpOrderWithBlindBoxDiscount(topup); err != nil {
		return nil, errors.New("创建订单失败")
	}
	callbackURL := strings.TrimRight(CallbackAddress(), "/") + "/api/nowpayments/ipn"
	returnURL := BuildPaymentReturnPath("/console/topup?pay=pending&show_history=true")
	payload := nowPaymentsPaymentRequest{
		PriceAmount:      topup.Money,
		PriceCurrency:    strings.ToLower(strings.TrimSpace(commercestore.NowPaymentsPaymentCurrency)),
		PayCurrency:      strings.ToLower(strings.TrimSpace(commercestore.NowPaymentsPayCurrency)),
		OrderID:          tradeNo,
		OrderDescription: fmt.Sprintf("账户充值 %d 额度", req.Amount),
		IPNCallbackURL:   callbackURL,
		SuccessURL:       returnURL,
		CancelURL:        returnURL,
	}
	body, err := platformencoding.Marshal(payload)
	if err != nil {
		_ = UpdatePendingTopUpStatus(tradeNo, PaymentProviderNowPayments, constant.TopUpStatusFailed)
		return nil, errors.New("创建支付请求失败")
	}
	reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodPost, nowPaymentsAPIBaseURL+"/payment", bytes.NewReader(body))
	if err != nil {
		_ = UpdatePendingTopUpStatus(tradeNo, PaymentProviderNowPayments, constant.TopUpStatusFailed)
		return nil, errors.New("创建支付请求失败")
	}
	reqHTTP.Header.Set("x-api-key", strings.TrimSpace(commercestore.NowPaymentsApiKey))
	reqHTTP.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(reqHTTP)
	if err != nil {
		_ = UpdatePendingTopUpStatus(tradeNo, PaymentProviderNowPayments, constant.TopUpStatusFailed)
		return nil, errors.New("拉起支付失败")
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = UpdatePendingTopUpStatus(tradeNo, PaymentProviderNowPayments, constant.TopUpStatusFailed)
		return nil, errors.New("NOWPayments 创建支付失败")
	}
	var payment nowPaymentsPaymentResponse
	if err := platformencoding.Unmarshal(responseBody, &payment); err != nil || strings.TrimSpace(payment.PayAddress) == "" {
		_ = UpdatePendingTopUpStatus(tradeNo, PaymentProviderNowPayments, constant.TopUpStatusFailed)
		return nil, errors.New("NOWPayments 返回无效")
	}
	paymentID, paymentIDErr := nowPaymentsRawNumber(payment.PaymentID)
	payAmount, payAmountErr := nowPaymentsRawNumber(payment.PayAmount)
	if paymentIDErr != nil || payAmountErr != nil || strings.TrimSpace(paymentID) == "" || strings.TrimSpace(payAmount) == "" {
		_ = UpdatePendingTopUpStatus(tradeNo, PaymentProviderNowPayments, constant.TopUpStatusFailed)
		return nil, errors.New("NOWPayments 返回无效")
	}
	if err := platformdb.DB.Model(&commerceschema.TopUp{}).Where("trade_no = ? AND status = ?", tradeNo, constant.TopUpStatusPending).Update("external_payment_id", paymentID).Error; err != nil {
		_ = UpdatePendingTopUpStatus(tradeNo, PaymentProviderNowPayments, constant.TopUpStatusFailed)
		return nil, errors.New("保存支付订单失败")
	}
	logger.LogInfo(ctx, fmt.Sprintf("NOWPayments 充值订单创建成功 user_id=%d trade_no=%s payment_id=%s amount=%d usdt=%.6f", userID, tradeNo, paymentID, req.Amount, topup.Money))
	return &NowPaymentsCheckoutPayload{PayURL: payment.InvoiceURL, OrderID: tradeNo, PaymentID: paymentID, PayAddress: payment.PayAddress, PayAmount: payAmount, PayCurrency: payment.PayCurrency, ExpirationEstimateDate: payment.ExpirationEstimateDate}, nil
}

func VerifyNowPaymentsIPNSignature(body []byte, signature string) bool {
	if len(body) == 0 || strings.TrimSpace(signature) == "" || strings.TrimSpace(commercestore.NowPaymentsIPNSecret) == "" {
		return false
	}
	var payload any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return false
	}
	canonical, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	mac := hmac.New(sha512.New, []byte(commercestore.NowPaymentsIPNSecret))
	_, _ = mac.Write(canonical)
	expected := hex.EncodeToString(mac.Sum(nil))
	provided := strings.TrimSpace(signature)
	if len(provided) != len(expected) {
		return false
	}
	return hmac.Equal([]byte(strings.ToLower(provided)), []byte(expected))
}

func nowPaymentsRawNumber(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", errors.New("missing value")
	}
	var value string
	if raw[0] == '"' {
		if err := json.Unmarshal(raw, &value); err != nil {
			return "", err
		}
		return value, nil
	}
	return string(raw), nil
}

func HandleNowPaymentsIPN(ctx context.Context, body []byte, signature string, clientIP string) error {
	if !IsNowPaymentsTopUpEnabled() || !VerifyNowPaymentsIPNSignature(body, signature) {
		return errors.New("invalid NOWPayments IPN")
	}
	var payload NowPaymentsIPNPayload
	if err := platformencoding.Unmarshal(body, &payload); err != nil {
		return errors.New("invalid NOWPayments payload")
	}
	tradeNo := strings.TrimSpace(payload.OrderID)
	if tradeNo == "" {
		return errors.New("missing NOWPayments order id")
	}
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)
	topup := GetTopUpByTradeNo(tradeNo)
	if topup == nil || topup.PaymentProvider != PaymentProviderNowPayments {
		return errors.New("NOWPayments order not found")
	}
	if topup.Status == constant.TopUpStatusSuccess {
		return nil
	}
	if payload.PaymentStatus != "finished" {
		return nil
	}
	paidRaw, err := nowPaymentsRawNumber(payload.ActuallyPaid)
	if err != nil {
		return errors.New("NOWPayments payment amount is insufficient")
	}
	paid, err := decimal.NewFromString(strings.TrimSpace(paidRaw))
	if err != nil || paid.LessThan(decimal.NewFromFloat(topup.Money).Sub(decimal.NewFromFloat(0.000001))) {
		return errors.New("NOWPayments payment amount is insufficient")
	}
	completedTopUp, quotaToAdd, err := CompleteTopUpByTradeNo(tradeNo, PaymentProviderNowPayments, "", "", "")
	if err != nil {
		return err
	}
	auditapp.RecordTopupLog(completedTopUp.UserId, fmt.Sprintf("NOWPayments USDT 充值成功，额度: %v，支付: %.6f USDT", logger.LogQuota(quotaToAdd), completedTopUp.Money), clientIP, completedTopUp.PaymentMethod, PaymentProviderNowPayments)
	paymentID, _ := nowPaymentsRawNumber(payload.PaymentID)
	logger.LogInfo(ctx, fmt.Sprintf("NOWPayments topup success trade_no=%s payment_id=%s user_id=%d", tradeNo, paymentID, completedTopUp.UserId))
	return nil
}
