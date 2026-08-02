package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sh2001sh/new-api/constant"
	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	commercestore "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformgeneral "github.com/sh2001sh/new-api/internal/platform/general"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
	platformtext "github.com/sh2001sh/new-api/internal/platform/textx"
	"github.com/shopspring/decimal"
	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/checkout/session"
	"github.com/thanhpk/randstr"
	waffo "github.com/waffo-com/waffo-go"
	"github.com/waffo-com/waffo-go/config"
	"github.com/waffo-com/waffo-go/types/order"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const CreemSignatureHeader = "creem-signature"

type StripePayRequest struct {
	Amount        int64  `json:"amount"`
	PaymentMethod string `json:"payment_method"`
	SuccessURL    string `json:"success_url,omitempty"`
	CancelURL     string `json:"cancel_url,omitempty"`
	WalletType    string `json:"wallet_type,omitempty"`
}

type CreemPayRequest struct {
	ProductID     string `json:"product_id"`
	PaymentMethod string `json:"payment_method"`
}

type CreemProduct struct {
	ProductID string  `json:"productId"`
	Name      string  `json:"name"`
	Price     float64 `json:"price"`
	Currency  string  `json:"currency"`
	Quota     int64   `json:"quota"`
}

type WaffoPayRequest struct {
	Amount         int64  `json:"amount"`
	WalletType     string `json:"wallet_type,omitempty"`
	PayMethodIndex *int   `json:"pay_method_index"`
	PayMethodType  string `json:"pay_method_type"`
	PayMethodName  string `json:"pay_method_name"`
}

type WaffoPancakePayRequest struct {
	Amount     int64  `json:"amount"`
	WalletType string `json:"wallet_type,omitempty"`
}

type StripeCheckoutPayload struct {
	PayLink string `json:"pay_link"`
	OrderID string `json:"order_id"`
}

type CreemCheckoutPayload struct {
	CheckoutURL string `json:"checkout_url"`
	OrderID     string `json:"order_id"`
}

type WaffoCheckoutPayload struct {
	PaymentURL string `json:"payment_url"`
	OrderID    string `json:"order_id"`
}

type WaffoPancakeCheckoutPayload struct {
	CheckoutURL string `json:"checkout_url"`
	SessionID   string `json:"session_id"`
	ExpiresAt   int64  `json:"expires_at"`
	OrderID     string `json:"order_id"`
}

type XunhuCheckoutPayload struct {
	PayURL    string `json:"pay_url"`
	QRCodeURL string `json:"qrcode_url"`
	OrderID   string `json:"order_id"`
}

type CreemWebhookEvent struct {
	ID        string `json:"id"`
	EventType string `json:"eventType"`
	CreatedAt int64  `json:"created_at"`
	Object    struct {
		ID        string `json:"id"`
		Object    string `json:"object"`
		RequestID string `json:"request_id"`
		Order     struct {
			Object      string `json:"object"`
			ID          string `json:"id"`
			Customer    string `json:"customer"`
			Product     string `json:"product"`
			Amount      int    `json:"amount"`
			Currency    string `json:"currency"`
			SubTotal    int    `json:"sub_total"`
			TaxAmount   int    `json:"tax_amount"`
			AmountDue   int    `json:"amount_due"`
			AmountPaid  int    `json:"amount_paid"`
			Status      string `json:"status"`
			Type        string `json:"type"`
			Transaction string `json:"transaction"`
			CreatedAt   string `json:"created_at"`
			UpdatedAt   string `json:"updated_at"`
			Mode        string `json:"mode"`
		} `json:"order"`
		Product struct {
			ID                string  `json:"id"`
			Object            string  `json:"object"`
			Name              string  `json:"name"`
			Description       string  `json:"description"`
			Price             int     `json:"price"`
			Currency          string  `json:"currency"`
			BillingType       string  `json:"billing_type"`
			BillingPeriod     string  `json:"billing_period"`
			Status            string  `json:"status"`
			TaxMode           string  `json:"tax_mode"`
			TaxCategory       string  `json:"tax_category"`
			DefaultSuccessURL *string `json:"default_success_url"`
			CreatedAt         string  `json:"created_at"`
			UpdatedAt         string  `json:"updated_at"`
			Mode              string  `json:"mode"`
		} `json:"product"`
		Units    int `json:"units"`
		Customer struct {
			ID        string `json:"id"`
			Object    string `json:"object"`
			Email     string `json:"email"`
			Name      string `json:"name"`
			Country   string `json:"country"`
			CreatedAt string `json:"created_at"`
			UpdatedAt string `json:"updated_at"`
			Mode      string `json:"mode"`
		} `json:"customer"`
		Status   string            `json:"status"`
		Metadata map[string]string `json:"metadata"`
		Mode     string            `json:"mode"`
	} `json:"object"`
}

func QuoteStripeTopUpAmount(userID int, req StripePayRequest) (string, error) {
	walletType := NormalizeTopupWalletType(req.WalletType)
	minTopup := GetStripeMinTopup()
	if IsClaudeTopupWallet(walletType) {
		minTopup = 1
	}
	if req.Amount < minTopup {
		return "", fmt.Errorf("充值数量不能小于 %d", GetStripeMinTopup())
	}

	group, err := loadCommerceUserGroup(userID, true)
	if err != nil {
		return "", errors.New("获取用户分组失败")
	}

	payMoney := GetStripePayMoney(float64(req.Amount), group)
	if IsClaudeTopupWallet(walletType) {
		payMoney = float64(req.Amount)
	}
	payMoney = ApplyTopupBlindBoxDiscount(userID, payMoney)
	if payMoney <= 0.01 {
		return "", errors.New("充值金额过低")
	}
	return strconv.FormatFloat(payMoney, 'f', 2, 64), nil
}

func CreateStripeTopUp(ctx context.Context, userID int, req StripePayRequest) (*StripeCheckoutPayload, error) {
	if req.PaymentMethod != commerceschema.PaymentMethodStripe {
		return nil, errors.New("不支持的支付渠道")
	}

	walletType := NormalizeTopupWalletType(req.WalletType)
	minTopup := GetStripeMinTopup()
	if IsClaudeTopupWallet(walletType) {
		minTopup = 1
	}
	if req.Amount < minTopup {
		return nil, fmt.Errorf("充值数量不能小于 %d", GetStripeMinTopup())
	}
	if req.Amount > 10000 {
		return nil, errors.New("充值数量不能大于 10000")
	}
	if req.SuccessURL != "" && platformsecurity.ValidateRedirectURL(req.SuccessURL) != nil {
		return nil, errors.New("支付成功重定向URL不在可信任域名列表中")
	}
	if req.CancelURL != "" && platformsecurity.ValidateRedirectURL(req.CancelURL) != nil {
		return nil, errors.New("支付取消重定向URL不在可信任域名列表中")
	}

	user, err := loadCommerceUserByID(userID, false)
	if err != nil || user == nil {
		return nil, errors.New("用户不存在")
	}
	group, err := loadCommerceUserGroup(userID, true)
	if err != nil {
		return nil, errors.New("获取用户分组失败")
	}

	chargedMoney := GetStripePayMoney(float64(req.Amount), group)
	if IsClaudeTopupWallet(walletType) {
		chargedMoney = float64(req.Amount)
	}

	reference := fmt.Sprintf("new-api-ref-%d-%d-%s", user.Id, time.Now().UnixMilli(), randstr.String(4))
	referenceID := "ref_" + platformsecurity.Sha1([]byte(reference))
	topUp := &commerceschema.TopUp{
		UserId:          userID,
		Amount:          req.Amount,
		Money:           chargedMoney,
		TradeNo:         referenceID,
		PaymentMethod:   commerceschema.PaymentMethodStripe,
		PaymentProvider: commerceschema.PaymentProviderStripe,
		WalletType:      walletType,
		CreateTime:      time.Now().Unix(),
		Status:          constant.TopUpStatusPending,
	}
	if _, err := CreatePendingTopUpOrderWithBlindBoxDiscount(topUp); err != nil {
		logger.LogError(ctx, fmt.Sprintf("Stripe 创建充值订单失败 user_id=%d trade_no=%s amount=%d error=%q", userID, referenceID, req.Amount, err.Error()))
		return nil, errors.New("创建订单失败")
	}

	payLink, err := genStripeLink(referenceID, user.StripeCustomer, user.Email, topUp.Money, req.Amount, walletType, req.SuccessURL, req.CancelURL)
	if err != nil {
		_ = UpdatePendingTopUpStatus(referenceID, commerceschema.PaymentProviderStripe, constant.TopUpStatusFailed)
		logger.LogError(ctx, fmt.Sprintf("Stripe 创建 Checkout Session 失败 user_id=%d trade_no=%s amount=%d error=%q", userID, referenceID, req.Amount, err.Error()))
		return nil, errors.New("拉起支付失败")
	}

	logger.LogInfo(ctx, fmt.Sprintf("Stripe 充值订单创建成功 user_id=%d trade_no=%s amount=%d money=%.2f", userID, referenceID, req.Amount, chargedMoney))
	return &StripeCheckoutPayload{PayLink: payLink, OrderID: referenceID}, nil
}

func HandleStripeWebhookFulfillment(ctx context.Context, referenceID string, customerID string, payload map[string]any, clientIP string) error {
	if len(referenceID) == 0 {
		logger.LogWarn(ctx, fmt.Sprintf("Stripe 完成订单时缺少订单号 client_ip=%s", clientIP))
		return nil
	}

	LockOrder(referenceID)
	defer UnlockOrder(referenceID)

	if topUp := GetTopUpByTradeNo(referenceID); topUp != nil && topUp.Status == constant.TopUpStatusSuccess {
		logger.LogInfo(ctx, fmt.Sprintf("Stripe webhook duplicate success ignored trade_no=%s client_ip=%s", referenceID, clientIP))
		return nil
	}
	if err := CompleteSubscriptionOrder(referenceID, platformtext.GetJsonString(payload), commerceschema.PaymentProviderStripe, ""); err == nil {
		logger.LogInfo(ctx, fmt.Sprintf("Stripe 订阅订单处理成功 trade_no=%s client_ip=%s", referenceID, clientIP))
		return nil
	} else if err != nil && !errors.Is(err, commerceschema.ErrSubscriptionOrderNotFound) {
		logger.LogError(ctx, fmt.Sprintf("Stripe 订阅订单处理失败 trade_no=%s client_ip=%s error=%q", referenceID, clientIP, err.Error()))
		return err
	}

	if err := Recharge(referenceID, customerID, clientIP); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "already") {
			logger.LogInfo(ctx, fmt.Sprintf("Stripe recharge duplicate ignored trade_no=%s client_ip=%s", referenceID, clientIP))
			return nil
		}
		logger.LogError(ctx, fmt.Sprintf("Stripe 充值处理失败 trade_no=%s client_ip=%s error=%q", referenceID, clientIP, err.Error()))
		return err
	}
	logger.LogInfo(ctx, fmt.Sprintf("Stripe 充值成功 trade_no=%s client_ip=%s", referenceID, clientIP))
	return nil
}

func MarkStripeTopUpFailed(ctx context.Context, referenceID string, clientIP string) error {
	if len(referenceID) == 0 {
		logger.LogWarn(ctx, fmt.Sprintf("Stripe 异步支付失败事件缺少订单号 client_ip=%s", clientIP))
		return nil
	}

	LockOrder(referenceID)
	defer UnlockOrder(referenceID)

	topUp := GetTopUpByTradeNo(referenceID)
	if topUp == nil {
		logger.LogWarn(ctx, fmt.Sprintf("Stripe 异步支付失败但本地订单不存在 trade_no=%s client_ip=%s", referenceID, clientIP))
		return nil
	}
	if topUp.PaymentProvider != commerceschema.PaymentProviderStripe {
		logger.LogWarn(ctx, fmt.Sprintf("Stripe 异步支付失败但订单支付网关不匹配 trade_no=%s payment_provider=%s client_ip=%s", referenceID, topUp.PaymentProvider, clientIP))
		return nil
	}
	if topUp.Status != constant.TopUpStatusPending {
		logger.LogInfo(ctx, fmt.Sprintf("Stripe 异步支付失败但订单状态非 pending，忽略处理 trade_no=%s status=%s client_ip=%s", referenceID, topUp.Status, clientIP))
		return nil
	}

	if err := UpdatePendingTopUpStatus(referenceID, commerceschema.PaymentProviderStripe, constant.TopUpStatusFailed); err != nil {
		logger.LogError(ctx, fmt.Sprintf("Stripe 标记充值订单失败状态失败 trade_no=%s client_ip=%s error=%q", referenceID, clientIP, err.Error()))
		return err
	}
	logger.LogInfo(ctx, fmt.Sprintf("Stripe 充值订单已标记为失败 trade_no=%s client_ip=%s", referenceID, clientIP))
	return nil
}

func ExpireStripeOrder(ctx context.Context, referenceID string) error {
	if len(referenceID) == 0 {
		logger.LogWarn(ctx, "Stripe checkout.expired 缺少订单号")
		return nil
	}

	LockOrder(referenceID)
	defer UnlockOrder(referenceID)

	if err := ExpireSubscriptionOrder(referenceID, commerceschema.PaymentProviderStripe); err == nil {
		logger.LogInfo(ctx, fmt.Sprintf("Stripe 订阅订单已过期 trade_no=%s", referenceID))
		return nil
	} else if err != nil && !errors.Is(err, commerceschema.ErrSubscriptionOrderNotFound) {
		logger.LogError(ctx, fmt.Sprintf("Stripe 订阅订单过期处理失败 trade_no=%s error=%q", referenceID, err.Error()))
		return err
	}

	err := UpdatePendingTopUpStatus(referenceID, commerceschema.PaymentProviderStripe, constant.TopUpStatusExpired)
	if errors.Is(err, commerceschema.ErrTopUpNotFound) {
		logger.LogWarn(ctx, fmt.Sprintf("Stripe 充值订单不存在，无法标记过期 trade_no=%s", referenceID))
		return nil
	}
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Stripe 充值订单过期处理失败 trade_no=%s error=%q", referenceID, err.Error()))
		return err
	}
	logger.LogInfo(ctx, fmt.Sprintf("Stripe 充值订单已过期 trade_no=%s", referenceID))
	return nil
}

func CreateCreemTopUp(ctx context.Context, userID int, req CreemPayRequest) (*CreemCheckoutPayload, error) {
	if req.PaymentMethod != commerceschema.PaymentMethodCreem {
		return nil, errors.New("不支持的支付渠道")
	}
	if req.ProductID == "" {
		return nil, errors.New("请选择产品")
	}

	var products []CreemProduct
	if err := json.Unmarshal([]byte(commercestore.CreemProducts), &products); err != nil {
		logger.LogError(ctx, fmt.Sprintf("Creem 产品配置解析失败 user_id=%d error=%q", userID, err.Error()))
		return nil, errors.New("产品配置错误")
	}

	var selectedProduct *CreemProduct
	for _, product := range products {
		if product.ProductID == req.ProductID {
			productCopy := product
			selectedProduct = &productCopy
			break
		}
	}
	if selectedProduct == nil {
		return nil, errors.New("产品不存在")
	}

	user, err := loadCommerceUserByID(userID, false)
	if err != nil || user == nil {
		return nil, errors.New("用户不存在")
	}
	reference := fmt.Sprintf("creem-api-ref-%d-%d-%s", user.Id, time.Now().UnixMilli(), randstr.String(4))
	referenceID := "ref_" + platformsecurity.Sha1([]byte(reference))
	topUp := &commerceschema.TopUp{
		UserId:          userID,
		Amount:          selectedProduct.Quota,
		Money:           selectedProduct.Price,
		TradeNo:         referenceID,
		PaymentMethod:   commerceschema.PaymentMethodCreem,
		PaymentProvider: commerceschema.PaymentProviderCreem,
		CreateTime:      time.Now().Unix(),
		Status:          constant.TopUpStatusPending,
	}
	if _, err := CreatePendingTopUpOrderWithBlindBoxDiscount(topUp); err != nil {
		logger.LogError(ctx, fmt.Sprintf("Creem 创建充值订单失败 user_id=%d trade_no=%s product_id=%s error=%q", userID, referenceID, selectedProduct.ProductID, err.Error()))
		return nil, errors.New("创建订单失败")
	}

	checkoutURL, err := genCreemLink(ctx, referenceID, selectedProduct, user.Email, user.Username, topUp.Money)
	if err != nil {
		_ = UpdatePendingTopUpStatus(referenceID, commerceschema.PaymentProviderCreem, constant.TopUpStatusFailed)
		logger.LogError(ctx, fmt.Sprintf("Creem 创建支付链接失败 user_id=%d trade_no=%s product_id=%s error=%q", userID, referenceID, selectedProduct.ProductID, err.Error()))
		return nil, errors.New("拉起支付失败")
	}
	logger.LogInfo(ctx, fmt.Sprintf("Creem 充值订单创建成功 user_id=%d trade_no=%s product_id=%s product_name=%q quota=%d money=%.2f", userID, referenceID, selectedProduct.ProductID, selectedProduct.Name, selectedProduct.Quota, topUp.Money))
	return &CreemCheckoutPayload{CheckoutURL: checkoutURL, OrderID: referenceID}, nil
}

func VerifyCreemSignature(payload string, signature string, secret string) bool {
	if secret == "" {
		logger.LogWarn(context.Background(), fmt.Sprintf("Creem webhook secret 未配置 test_mode=%t signature=%q body=%q", commercestore.CreemTestMode, signature, payload))
		if commercestore.CreemTestMode {
			logger.LogInfo(context.Background(), fmt.Sprintf("Creem webhook 验签已跳过 reason=test_mode signature=%q body=%q", signature, payload))
			return true
		}
		return false
	}

	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(payload))
	expectedSignature := hex.EncodeToString(h.Sum(nil))
	return hmac.Equal([]byte(signature), []byte(expectedSignature))
}

func HandleCreemCheckoutCompleted(ctx context.Context, event *CreemWebhookEvent, clientIP string) error {
	if event.Object.Order.Status != "paid" {
		logger.LogInfo(ctx, fmt.Sprintf("Creem 订单状态未支付，忽略处理 request_id=%s order_id=%s order_status=%s", event.Object.RequestID, event.Object.Order.ID, event.Object.Order.Status))
		return nil
	}
	referenceID := event.Object.RequestID
	if referenceID == "" {
		logger.LogWarn(ctx, fmt.Sprintf("Creem webhook 缺少 request_id event_id=%s order_id=%s", event.ID, event.Object.Order.ID))
		return errors.New("missing request_id")
	}

	LockOrder(referenceID)
	defer UnlockOrder(referenceID)

	if err := CompleteSubscriptionOrder(referenceID, platformtext.GetJsonString(event), commerceschema.PaymentProviderCreem, ""); err == nil {
		logger.LogInfo(ctx, fmt.Sprintf("Creem 订阅订单处理成功 trade_no=%s creem_order_id=%s", referenceID, event.Object.Order.ID))
		return nil
	} else if err != nil && !errors.Is(err, commerceschema.ErrSubscriptionOrderNotFound) {
		logger.LogError(ctx, fmt.Sprintf("Creem 订阅订单处理失败 trade_no=%s creem_order_id=%s error=%q", referenceID, event.Object.Order.ID, err.Error()))
		return err
	}

	if event.Object.Order.Type != "onetime" {
		logger.LogInfo(ctx, fmt.Sprintf("Creem 暂不支持该订单类型，忽略处理 request_id=%s creem_order_id=%s order_type=%s", referenceID, event.Object.Order.ID, event.Object.Order.Type))
		return nil
	}

	topUp := GetTopUpByTradeNo(referenceID)
	if topUp == nil {
		logger.LogWarn(ctx, fmt.Sprintf("Creem 充值订单不存在 trade_no=%s creem_order_id=%s", referenceID, event.Object.Order.ID))
		return nil
	}
	if topUp.Status != constant.TopUpStatusPending {
		logger.LogInfo(ctx, fmt.Sprintf("Creem 充值订单状态非 pending，忽略处理 trade_no=%s status=%s creem_order_id=%s", referenceID, topUp.Status, event.Object.Order.ID))
		return nil
	}

	err := RechargeCreem(referenceID, event.Object.Customer.Email, event.Object.Customer.Name, clientIP)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "already") {
			logger.LogInfo(ctx, fmt.Sprintf("Creem duplicate recharge ignored trade_no=%s creem_order_id=%s", referenceID, event.Object.Order.ID))
			return nil
		}
		logger.LogError(ctx, fmt.Sprintf("Creem 充值处理失败 trade_no=%s creem_order_id=%s client_ip=%s error=%q", referenceID, event.Object.Order.ID, clientIP, err.Error()))
		return err
	}

	logger.LogInfo(ctx, fmt.Sprintf("Creem 充值成功 trade_no=%s creem_order_id=%s quota=%d money=%.2f client_ip=%s", referenceID, event.Object.Order.ID, topUp.Amount, topUp.Money, clientIP))
	return nil
}

func QuoteWaffoTopUpAmount(userID int, req WaffoPayRequest) (string, error) {
	walletType := NormalizeTopupWalletType(req.WalletType)
	minTopup := int64(commercestore.WaffoMinTopUp)
	if IsClaudeTopupWallet(walletType) {
		minTopup = 1
	}
	if req.Amount < minTopup {
		return "", fmt.Errorf("充值数量不能小于 %d", minTopup)
	}

	group, err := loadCommerceUserGroup(userID, true)
	if err != nil {
		return "", errors.New("获取用户分组失败")
	}
	payMoney := GetWaffoPayMoney(float64(req.Amount), group)
	if IsClaudeTopupWallet(walletType) {
		payMoney = float64(req.Amount)
	}
	payMoney = ApplyTopupBlindBoxDiscount(userID, payMoney)
	if payMoney <= 0.01 {
		return "", errors.New("充值金额过低")
	}
	return strconv.FormatFloat(payMoney, 'f', 2, 64), nil
}

func CreateWaffoTopUp(ctx context.Context, userID int, req WaffoPayRequest) (*WaffoCheckoutPayload, error) {
	if !commercestore.WaffoEnabled {
		return nil, errors.New("Waffo 支付未启用")
	}
	walletType := NormalizeTopupWalletType(req.WalletType)
	minTopup := int64(commercestore.WaffoMinTopUp)
	if IsClaudeTopupWallet(walletType) {
		minTopup = 1
	}
	if req.Amount < minTopup {
		return nil, fmt.Errorf("充值数量不能小于 %d", minTopup)
	}

	user, err := loadCommerceUserByID(userID, false)
	if err != nil || user == nil {
		return nil, errors.New("用户不存在")
	}

	var resolvedPayMethodType, resolvedPayMethodName string
	methods := commercestore.GetWaffoPayMethods()
	if req.PayMethodIndex != nil {
		idx := *req.PayMethodIndex
		if idx < 0 || idx >= len(methods) {
			logger.LogWarn(ctx, fmt.Sprintf("Waffo 支付方式索引无效 user_id=%d pay_method_index=%d method_count=%d", userID, idx, len(methods)))
			return nil, errors.New("不支持的支付方式")
		}
		resolvedPayMethodType = methods[idx].PayMethodType
		resolvedPayMethodName = methods[idx].PayMethodName
	} else if req.PayMethodType != "" {
		valid := false
		for _, method := range methods {
			if method.PayMethodType == req.PayMethodType && method.PayMethodName == req.PayMethodName {
				valid = true
				resolvedPayMethodType = method.PayMethodType
				resolvedPayMethodName = method.PayMethodName
				break
			}
		}
		if !valid {
			logger.LogWarn(ctx, fmt.Sprintf("Waffo 支付方式无效 user_id=%d pay_method_type=%s pay_method_name=%q", userID, req.PayMethodType, req.PayMethodName))
			return nil, errors.New("不支持的支付方式")
		}
	}

	group, err := loadCommerceUserGroup(userID, true)
	if err != nil {
		return nil, errors.New("获取用户分组失败")
	}
	payMoney := GetWaffoPayMoney(float64(req.Amount), group)
	if IsClaudeTopupWallet(walletType) {
		payMoney = float64(req.Amount)
	}
	payMoney = ApplyTopupBlindBoxDiscount(userID, payMoney)
	if payMoney < 0.01 {
		return nil, errors.New("充值金额过低")
	}

	merchantOrderID := fmt.Sprintf("WAFFO-%d-%d-%s", userID, time.Now().UnixMilli(), randstr.String(6))
	paymentRequestID := merchantOrderID
	amount := req.Amount
	if !IsClaudeTopupWallet(walletType) && platformgeneral.GetQuotaDisplayType() == platformgeneral.QuotaDisplayTypeTokens {
		amount = int64(float64(req.Amount) / platformruntime.QuotaPerUnit)
		if amount < 1 {
			amount = 1
		}
	}
	topUp := &commerceschema.TopUp{
		UserId:          userID,
		Amount:          amount,
		Money:           payMoney,
		TradeNo:         merchantOrderID,
		PaymentMethod:   commerceschema.PaymentMethodWaffo,
		PaymentProvider: commerceschema.PaymentProviderWaffo,
		WalletType:      walletType,
		CreateTime:      time.Now().Unix(),
		Status:          constant.TopUpStatusPending,
	}
	if _, err := CreatePendingTopUpOrderWithBlindBoxDiscount(topUp); err != nil {
		logger.LogError(ctx, fmt.Sprintf("Waffo 创建充值订单失败 user_id=%d trade_no=%s amount=%d error=%q", userID, merchantOrderID, req.Amount, err.Error()))
		return nil, errors.New("创建订单失败")
	}

	sdk, err := GetWaffoSDK()
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Waffo SDK 初始化失败 user_id=%d trade_no=%s error=%q", userID, merchantOrderID, err.Error()))
		_ = UpdatePendingTopUpStatus(merchantOrderID, commerceschema.PaymentProviderWaffo, constant.TopUpStatusFailed)
		return nil, errors.New("支付配置错误")
	}

	callbackAddr := CallbackAddress()
	notifyURL := callbackAddr + "/api/waffo/webhook"
	if commercestore.WaffoNotifyUrl != "" {
		notifyURL = commercestore.WaffoNotifyUrl
	}
	returnURL := BuildPaymentReturnPath("/console/topup?show_history=true")
	if commercestore.WaffoReturnUrl != "" {
		returnURL = commercestore.WaffoReturnUrl
	}
	currency := getWaffoCurrency()
	createParams := &order.CreateOrderParams{
		PaymentRequestID: paymentRequestID,
		MerchantOrderID:  merchantOrderID,
		OrderAmount:      formatWaffoAmount(payMoney, currency),
		OrderCurrency:    currency,
		OrderDescription: fmt.Sprintf("Recharge %d credits", req.Amount),
		OrderRequestedAt: time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		NotifyURL:        notifyURL,
		MerchantInfo: &order.MerchantInfo{
			MerchantID: commercestore.WaffoMerchantId,
		},
		UserInfo: &order.UserInfo{
			UserID:       strconv.Itoa(user.Id),
			UserEmail:    getWaffoUserEmail(user),
			UserTerminal: "WEB",
		},
		PaymentInfo: &order.PaymentInfo{
			ProductName:   "ONE_TIME_PAYMENT",
			PayMethodType: resolvedPayMethodType,
			PayMethodName: resolvedPayMethodName,
		},
		SuccessRedirectURL: returnURL,
		FailedRedirectURL:  returnURL,
	}
	resp, err := sdk.Order().Create(ctx, createParams, nil)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Waffo 创建订单失败 user_id=%d trade_no=%s error=%q", userID, merchantOrderID, err.Error()))
		_ = UpdatePendingTopUpStatus(merchantOrderID, commerceschema.PaymentProviderWaffo, constant.TopUpStatusFailed)
		return nil, errors.New("拉起支付失败")
	}
	if !resp.IsSuccess() {
		logger.LogWarn(ctx, fmt.Sprintf("Waffo 创建订单业务失败 user_id=%d trade_no=%s code=%s message=%q response=%q", userID, merchantOrderID, resp.Code, resp.Message, platformtext.GetJsonString(resp)))
		_ = UpdatePendingTopUpStatus(merchantOrderID, commerceschema.PaymentProviderWaffo, constant.TopUpStatusFailed)
		return nil, errors.New("拉起支付失败")
	}

	orderData := resp.GetData()
	paymentURL := orderData.FetchRedirectURL()
	if paymentURL == "" {
		paymentURL = orderData.OrderAction
	}
	logger.LogInfo(ctx, fmt.Sprintf("Waffo 充值订单创建成功 user_id=%d trade_no=%s amount=%d money=%.2f pay_method_type=%s pay_method_name=%q", userID, merchantOrderID, req.Amount, payMoney, resolvedPayMethodType, resolvedPayMethodName))
	return &WaffoCheckoutPayload{PaymentURL: paymentURL, OrderID: merchantOrderID}, nil
}

func HandleWaffoPaymentStatus(ctx context.Context, merchantOrderID string, orderStatus string, clientIP string) error {
	if orderStatus != "PAY_SUCCESS" {
		logger.LogInfo(ctx, fmt.Sprintf("Waffo 订单状态非成功，忽略充值 trade_no=%s order_status=%s client_ip=%s", merchantOrderID, orderStatus, clientIP))
		if merchantOrderID != "" {
			if err := UpdatePendingTopUpStatus(merchantOrderID, commerceschema.PaymentProviderWaffo, constant.TopUpStatusFailed); err != nil &&
				!errors.Is(err, commerceschema.ErrTopUpNotFound) &&
				!errors.Is(err, commerceschema.ErrTopUpStatusInvalid) {
				logger.LogError(ctx, fmt.Sprintf("Waffo 标记失败订单状态失败 trade_no=%s error=%q", merchantOrderID, err.Error()))
				return err
			}
		}
		return nil
	}

	LockOrder(merchantOrderID)
	defer UnlockOrder(merchantOrderID)
	if err := RechargeWaffo(merchantOrderID, clientIP); err != nil {
		logger.LogError(ctx, fmt.Sprintf("Waffo 充值处理失败 trade_no=%s client_ip=%s error=%q", merchantOrderID, clientIP, err.Error()))
		return err
	}
	logger.LogInfo(ctx, fmt.Sprintf("Waffo 充值成功 trade_no=%s client_ip=%s", merchantOrderID, clientIP))
	return nil
}

func QuoteWaffoPancakeTopUpAmount(userID int, req WaffoPancakePayRequest) (string, error) {
	walletType := NormalizeTopupWalletType(req.WalletType)
	minTopup := int64(commercestore.WaffoPancakeMinTopUp)
	if IsClaudeTopupWallet(walletType) {
		minTopup = 1
	}
	if req.Amount < minTopup {
		return "", fmt.Errorf("充值数量不能小于 %d", commercestore.WaffoPancakeMinTopUp)
	}

	group, err := loadCommerceUserGroup(userID, true)
	if err != nil {
		return "", errors.New("获取用户分组失败")
	}
	payMoney := GetWaffoPancakePayMoney(req.Amount, group)
	if IsClaudeTopupWallet(walletType) {
		payMoney = float64(req.Amount)
	}
	payMoney = ApplyTopupBlindBoxDiscount(userID, payMoney)
	if payMoney <= 0.01 {
		return "", errors.New("充值金额过低")
	}
	return fmt.Sprintf("%.2f", payMoney), nil
}

func CreateWaffoPancakeTopUp(ctx context.Context, userID int, req WaffoPancakePayRequest) (*WaffoPancakeCheckoutPayload, error) {
	if !commercestore.WaffoPancakeEnabled {
		return nil, errors.New("Waffo Pancake 支付未启用")
	}
	currentWebhookKey := commercestore.WaffoPancakeWebhookPublicKey
	if commercestore.WaffoPancakeSandbox {
		currentWebhookKey = commercestore.WaffoPancakeWebhookTestKey
	}
	if strings.TrimSpace(commercestore.WaffoPancakeMerchantID) == "" ||
		strings.TrimSpace(commercestore.WaffoPancakePrivateKey) == "" ||
		strings.TrimSpace(currentWebhookKey) == "" ||
		strings.TrimSpace(commercestore.WaffoPancakeStoreID) == "" ||
		strings.TrimSpace(commercestore.WaffoPancakeProductID) == "" {
		return nil, errors.New("Waffo Pancake 配置不完整")
	}

	walletType := NormalizeTopupWalletType(req.WalletType)
	minTopup := int64(commercestore.WaffoPancakeMinTopUp)
	if IsClaudeTopupWallet(walletType) {
		minTopup = 1
	}
	if req.Amount < minTopup {
		return nil, fmt.Errorf("充值数量不能小于 %d", commercestore.WaffoPancakeMinTopUp)
	}

	user, err := loadCommerceUserByID(userID, false)
	if err != nil || user == nil {
		return nil, errors.New("用户不存在")
	}
	group, err := loadCommerceUserGroup(userID, true)
	if err != nil {
		return nil, errors.New("获取用户分组失败")
	}
	payMoney := GetWaffoPancakePayMoney(req.Amount, group)
	if IsClaudeTopupWallet(walletType) {
		payMoney = float64(req.Amount)
	}
	if payMoney < 0.01 {
		return nil, errors.New("充值金额过低")
	}

	tradeNo := fmt.Sprintf("WAFFO_PANCAKE-%d-%d-%s", userID, time.Now().UnixMilli(), randstr.String(6))
	topUp := &commerceschema.TopUp{
		UserId:          userID,
		Amount:          NormalizeStoredTopupAmount(req.Amount, walletType),
		Money:           payMoney,
		TradeNo:         tradeNo,
		PaymentMethod:   commerceschema.PaymentMethodWaffoPancake,
		PaymentProvider: commerceschema.PaymentProviderWaffoPancake,
		WalletType:      walletType,
		CreateTime:      time.Now().Unix(),
		Status:          constant.TopUpStatusPending,
	}
	if _, err := CreatePendingTopUpOrderWithBlindBoxDiscount(topUp); err != nil {
		logger.LogError(ctx, fmt.Sprintf("Waffo Pancake 创建充值订单失败 user_id=%d trade_no=%s amount=%d error=%q", userID, tradeNo, req.Amount, err.Error()))
		return nil, errors.New("创建订单失败")
	}

	expiresInSeconds := 45 * 60
	sessionData, err := CreateWaffoPancakeCheckoutSession(ctx, &WaffoPancakeCreateSessionParams{
		StoreID:     commercestore.WaffoPancakeStoreID,
		ProductID:   commercestore.WaffoPancakeProductID,
		ProductType: "onetime",
		Currency:    strings.ToUpper(strings.TrimSpace(commercestore.WaffoPancakeCurrency)),
		PriceSnapshot: &WaffoPancakePriceSnapshot{
			Amount:      formatWaffoPancakeAmount(payMoney),
			TaxIncluded: false,
			TaxCategory: "saas",
		},
		BuyerEmail:       getWaffoPancakeBuyerEmail(user),
		SuccessURL:       getWaffoPancakeReturnURL(),
		ExpiresInSeconds: &expiresInSeconds,
	})
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Waffo Pancake 创建结账会话失败 user_id=%d trade_no=%s error=%q", userID, tradeNo, err.Error()))
		_ = UpdatePendingTopUpStatus(tradeNo, commerceschema.PaymentProviderWaffoPancake, constant.TopUpStatusFailed)
		return nil, errors.New("拉起支付失败")
	}
	logger.LogInfo(ctx, fmt.Sprintf("Waffo Pancake 充值订单创建成功 user_id=%d trade_no=%s session_id=%s amount=%d money=%.2f", userID, tradeNo, sessionData.SessionID, req.Amount, payMoney))
	return &WaffoPancakeCheckoutPayload{
		CheckoutURL: sessionData.CheckoutURL,
		SessionID:   sessionData.SessionID,
		ExpiresAt:   0,
		OrderID:     tradeNo,
	}, nil
}

func CompleteWaffoPancakeTopUp(ctx context.Context, tradeNo string, eventID string, orderID string, clientIP string) error {
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)
	if err := RechargeWaffoPancake(tradeNo); err != nil {
		logger.LogError(ctx, fmt.Sprintf("Waffo Pancake 充值处理失败 trade_no=%s event_id=%s order_id=%s client_ip=%s error=%q", tradeNo, eventID, orderID, clientIP, err.Error()))
		return err
	}
	logger.LogInfo(ctx, fmt.Sprintf("Waffo Pancake 充值成功 trade_no=%s event_id=%s order_id=%s client_ip=%s", tradeNo, eventID, orderID, clientIP))
	return nil
}

func CreateXunhuTopUp(ctx context.Context, userID int, req EpayRequest) (*XunhuCheckoutPayload, error) {
	walletType := NormalizeTopupWalletType(req.WalletType)
	minTopup := GetTopupMinAmount(walletType)
	if req.Amount < minTopup {
		return nil, fmt.Errorf("minimum top-up is %d", minTopup)
	}

	group, err := loadCommerceUserGroup(userID, true)
	if err != nil {
		return nil, errors.New("failed to get user group")
	}
	payMoney := GetTopupPayMoney(req.Amount, group, walletType)
	payMoney = ApplyTopupBlindBoxDiscount(userID, payMoney)
	if payMoney < 0.01 {
		return nil, errors.New("payment amount is too low")
	}
	if commercestore.XunhuMinTopUp > 0 && payMoney < float64(commercestore.XunhuMinTopUp) {
		return nil, fmt.Errorf("minimum XunhuPay payment is %d CNY", commercestore.XunhuMinTopUp)
	}

	callbackAddress := CallbackAddress()
	tradeNo := fmt.Sprintf("USR%dNO%s%d", userID, platformruntime.GetRandomString(6), time.Now().Unix())
	notifyURL := callbackAddress + "/api/user/xunhu/notify"
	returnURL := callbackAddress + "/api/user/xunhu/return?trade_no=" + tradeNo
	amount := NormalizeStoredTopupAmount(req.Amount, walletType)
	topup := &commerceschema.TopUp{
		UserId:          userID,
		Amount:          amount,
		Money:           payMoney,
		TradeNo:         tradeNo,
		PaymentMethod:   commerceschema.PaymentMethodXunhu,
		PaymentProvider: commerceschema.PaymentProviderXunhu,
		WalletType:      walletType,
		CreateTime:      time.Now().Unix(),
		Status:          constant.TopUpStatusPending,
	}
	if _, err := CreatePendingTopUpOrderWithBlindBoxDiscount(topup); err != nil {
		logger.LogError(ctx, fmt.Sprintf("xunhu create topup db order failed user_id=%d trade_no=%s error=%q", userID, tradeNo, err.Error()))
		return nil, errors.New("failed to create order")
	}

	orderData, err := CreateXunhuOrder(tradeNo, fmt.Sprintf("TUC%d", req.Amount), topup.Money, notifyURL, returnURL)
	if err != nil {
		_ = UpdatePendingTopUpStatus(tradeNo, commerceschema.PaymentProviderXunhu, constant.TopUpStatusFailed)
		logger.LogError(ctx, fmt.Sprintf("xunhu create topup order failed user_id=%d trade_no=%s error=%q", userID, tradeNo, err.Error()))
		return nil, errors.New(FormatXunhuCreatePaymentError(err))
	}
	return &XunhuCheckoutPayload{PayURL: orderData.PayURL, QRCodeURL: orderData.QRCodeURL, OrderID: tradeNo}, nil
}

func HandleXunhuTopUpWebhook(ctx context.Context, params map[string]string, clientIP string) (bool, error) {
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
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)

	topup := GetTopUpByTradeNo(tradeNo)
	if topup == nil || topup.PaymentProvider != commerceschema.PaymentProviderXunhu {
		return false, nil
	}
	if topup.Status == constant.TopUpStatusSuccess {
		return true, nil
	}

	completedTopUp, quotaToAdd, err := CompleteTopUpByTradeNo(tradeNo, commerceschema.PaymentProviderXunhu, "", "", "")
	if err != nil {
		return false, err
	}
	auditapp.RecordTopupLog(completedTopUp.UserId, fmt.Sprintf("xunhu top-up success, wallet: %s, quota: %v, paid: %.2f", completedTopUp.NormalizedWalletType(), logger.LogQuota(quotaToAdd), completedTopUp.Money), clientIP, completedTopUp.PaymentMethod, commerceschema.PaymentProviderXunhu)
	logger.LogInfo(ctx, fmt.Sprintf("xunhu topup success trade_no=%s user_id=%d client_ip=%s", completedTopUp.TradeNo, completedTopUp.UserId, clientIP))
	return true, nil
}

func ResolveXunhuTopUpReturnURL(tradeNo string) string {
	topup := GetTopUpByTradeNo(tradeNo)
	if topup != nil && topup.Status == constant.TopUpStatusSuccess {
		return BuildPaymentReturnPath("/console/topup?pay=success&show_history=true")
	}
	return BuildPaymentReturnPath("/console/topup?pay=pending&show_history=true")
}

func GetWaffoSDK() (*waffo.Waffo, error) {
	env := config.Sandbox
	apiKey := commercestore.WaffoSandboxApiKey
	privateKey := commercestore.WaffoSandboxPrivateKey
	publicKey := commercestore.WaffoSandboxPublicCert
	if !commercestore.WaffoSandbox {
		env = config.Production
		apiKey = commercestore.WaffoApiKey
		privateKey = commercestore.WaffoPrivateKey
		publicKey = commercestore.WaffoPublicCert
	}
	builder := config.NewConfigBuilder().
		APIKey(apiKey).
		PrivateKey(privateKey).
		WaffoPublicKey(publicKey).
		Environment(env)
	if commercestore.WaffoMerchantId != "" {
		builder = builder.MerchantID(commercestore.WaffoMerchantId)
	}
	cfg, err := builder.Build()
	if err != nil {
		return nil, err
	}
	return waffo.New(cfg), nil
}

func StripeMoneyToMinorUnits(amount float64) int64 {
	if amount <= 0 {
		return 0
	}
	return int64(math.Round(amount * 100))
}

func genStripeLink(referenceID string, customerID string, email string, money float64, amount int64, walletType string, successURL string, cancelURL string) (string, error) {
	if !strings.HasPrefix(commercestore.StripeApiSecret, "sk_") && !strings.HasPrefix(commercestore.StripeApiSecret, "rk_") {
		return "", fmt.Errorf("无效的Stripe API密钥")
	}
	stripe.Key = commercestore.StripeApiSecret
	if successURL == "" {
		successURL = BuildPaymentReturnPath("/console/log")
	}
	if cancelURL == "" {
		cancelURL = BuildPaymentReturnPath("/console/topup")
	}
	productName := fmt.Sprintf("账户充值 %d", amount)
	if IsClaudeTopupWallet(walletType) {
		productName = fmt.Sprintf("Claude 额度充值 %d", amount)
	}
	unitAmount := StripeMoneyToMinorUnits(money)
	if unitAmount <= 0 {
		return "", fmt.Errorf("invalid stripe amount")
	}

	params := &stripe.CheckoutSessionParams{
		ClientReferenceID: stripe.String(referenceID),
		SuccessURL:        stripe.String(successURL),
		CancelURL:         stripe.String(cancelURL),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Quantity: stripe.Int64(1),
				PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
					Currency:   stripe.String("usd"),
					UnitAmount: stripe.Int64(unitAmount),
					ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
						Name: stripe.String(productName),
					},
				},
			},
		},
		Mode:                stripe.String(string(stripe.CheckoutSessionModePayment)),
		AllowPromotionCodes: stripe.Bool(commercestore.StripePromotionCodesEnabled),
	}
	if customerID == "" {
		if email != "" {
			params.CustomerEmail = stripe.String(email)
		}
		params.CustomerCreation = stripe.String(string(stripe.CheckoutSessionCustomerCreationAlways))
	} else {
		params.Customer = stripe.String(customerID)
	}
	result, err := session.New(params)
	if err != nil {
		return "", err
	}
	return result.URL, nil
}

func genCreemLink(ctx context.Context, referenceID string, product *CreemProduct, email string, username string, customPrice float64) (string, error) {
	if commercestore.CreemApiKey == "" {
		return "", fmt.Errorf("未配置Creem API密钥")
	}

	apiURL := "https://api.creem.io/v1/checkouts"
	if commercestore.CreemTestMode {
		apiURL = "https://test-api.creem.io/v1/checkouts"
		logger.LogInfo(ctx, fmt.Sprintf("Creem 使用测试环境 api_url=%s", apiURL))
	}

	requestData := CreemCheckoutRequest{
		ProductID: product.ProductID,
		RequestID: referenceID,
		Units:     1,
		Customer: struct {
			Email string `json:"email"`
		}{
			Email: email,
		},
		Metadata: map[string]string{
			"username":     username,
			"reference_id": referenceID,
			"product_name": product.Name,
			"quota":        fmt.Sprintf("%d", product.Quota),
		},
	}
	if cents := creemPriceToCents(customPrice); cents > 0 {
		requestData.CustomPrice = &cents
	}

	jsonData, err := json.Marshal(requestData)
	if err != nil {
		return "", fmt.Errorf("序列化请求数据失败: %v", err)
	}
	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("创建HTTP请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", commercestore.CreemApiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("发送HTTP请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %v", err)
	}
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("Creem API http status %d ", resp.StatusCode)
	}
	var checkoutResp CreemCheckoutResponse
	if err := json.Unmarshal(body, &checkoutResp); err != nil {
		return "", fmt.Errorf("解析响应失败: %v", err)
	}
	if checkoutResp.CheckoutURL == "" {
		return "", fmt.Errorf("Creem API resp no checkout url ")
	}
	return checkoutResp.CheckoutURL, nil
}

func GenCreemLink(ctx context.Context, referenceID string, product *CreemProduct, email string, username string, customPrice float64) (string, error) {
	return genCreemLink(ctx, referenceID, product, email, username, customPrice)
}

type CreemCheckoutRequest struct {
	ProductID   string `json:"product_id"`
	RequestID   string `json:"request_id"`
	Units       int    `json:"units,omitempty"`
	CustomPrice *int64 `json:"custom_price,omitempty"`
	Customer    struct {
		Email string `json:"email"`
	} `json:"customer"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type CreemCheckoutResponse struct {
	CheckoutURL string `json:"checkout_url"`
	ID          string `json:"id"`
}

func creemPriceToCents(price float64) int64 {
	if price <= 0 {
		return 0
	}
	return int64(math.Round(price * 100))
}

func getWaffoUserEmail(user *identityschema.User) string {
	return fmt.Sprintf("%d@examples.com", user.Id)
}

func getWaffoCurrency() string {
	if commercestore.WaffoCurrency != "" {
		return commercestore.WaffoCurrency
	}
	return "USD"
}

var zeroDecimalCurrencies = map[string]bool{
	"IDR": true, "JPY": true, "KRW": true, "VND": true,
}

func formatWaffoAmount(amount float64, currency string) string {
	if zeroDecimalCurrencies[currency] {
		return fmt.Sprintf("%.0f", amount)
	}
	return fmt.Sprintf("%.2f", amount)
}

func formatWaffoPancakeAmount(payMoney float64) string {
	return decimal.NewFromFloat(payMoney).StringFixed(2)
}

func getWaffoPancakeBuyerEmail(user *identityschema.User) string {
	if user != nil && strings.TrimSpace(user.Email) != "" {
		return user.Email
	}
	if user != nil {
		return fmt.Sprintf("%d@new-api.local", user.Id)
	}
	return ""
}

func getWaffoPancakeReturnURL() string {
	if strings.TrimSpace(commercestore.WaffoPancakeReturnURL) != "" {
		return commercestore.WaffoPancakeReturnURL
	}
	return BuildPaymentReturnPath("/console/topup?show_history=true")
}
