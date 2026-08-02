package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/sh2001sh/new-api/constant"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commercestore "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformgeneral "github.com/sh2001sh/new-api/internal/platform/general"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
	platformtext "github.com/sh2001sh/new-api/internal/platform/textx"
	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/checkout/session"
	"github.com/thanhpk/randstr"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type SubscriptionPurchaseFields struct {
	PlanID         int    `json:"plan_id"`
	PaymentMethod  string `json:"payment_method"`
	PurchaseType   string `json:"purchase_type"`
	GroupBuyID     int64  `json:"group_buy_id"`
	SubscriptionID int    `json:"subscription_id"`
	Quota          int64  `json:"quota"`
}

type SubscriptionEpayPayRequest struct {
	PlanID         int    `json:"plan_id"`
	PaymentMethod  string `json:"payment_method"`
	PurchaseType   string `json:"purchase_type"`
	GroupBuyID     int64  `json:"group_buy_id"`
	SubscriptionID int    `json:"subscription_id"`
	Quota          int64  `json:"quota"`
}

type SubscriptionXunhuPayRequest struct {
	PlanID         int    `json:"plan_id"`
	PurchaseType   string `json:"purchase_type"`
	GroupBuyID     int64  `json:"group_buy_id"`
	SubscriptionID int    `json:"subscription_id"`
	Quota          int64  `json:"quota"`
}

type SubscriptionStripePayRequest struct {
	PlanID         int    `json:"plan_id"`
	PurchaseType   string `json:"purchase_type"`
	GroupBuyID     int64  `json:"group_buy_id"`
	SubscriptionID int    `json:"subscription_id"`
	Quota          int64  `json:"quota"`
}

type SubscriptionCreemPayRequest struct {
	PlanID         int    `json:"plan_id"`
	PurchaseType   string `json:"purchase_type"`
	GroupBuyID     int64  `json:"group_buy_id"`
	SubscriptionID int    `json:"subscription_id"`
	Quota          int64  `json:"quota"`
}

type SubscriptionCheckoutPayload struct {
	OrderID   string  `json:"order_id"`
	AmountDue float64 `json:"amount_due"`
	Action    string  `json:"action"`
}

type SubscriptionEpayCheckoutPayload struct {
	SubscriptionCheckoutPayload
	Form map[string]string `json:"form"`
	URL  string            `json:"-"`
}

type SubscriptionXunhuCheckoutPayload struct {
	SubscriptionCheckoutPayload
	PayURL    string `json:"pay_url"`
	QRCodeURL string `json:"qrcode_url"`
}

type SubscriptionStripeCheckoutPayload struct {
	SubscriptionCheckoutPayload
	PayLink string `json:"pay_link"`
}

type SubscriptionCreemCheckoutPayload struct {
	SubscriptionCheckoutPayload
	CheckoutURL string `json:"checkout_url"`
}

func NormalizeSubscriptionPurchaseFields(userID int, req SubscriptionPurchaseFields) (string, int64, error) {
	purchaseType := commercedomain.NormalizeSubscriptionPurchaseType(req.PurchaseType)
	groupBuyID := req.GroupBuyID
	if purchaseType != commerceschema.SubscriptionPurchaseTypeJoinGroup {
		groupBuyID = 0
	}
	if err := ValidateGroupBuyPurchase(userID, req.PlanID, purchaseType, groupBuyID); err != nil {
		return "", 0, err
	}
	return purchaseType, groupBuyID, nil
}

func ApplySubscriptionPurchaseFields(order *commerceschema.SubscriptionOrder, purchaseType string, groupBuyID int64) {
	if order == nil {
		return
	}
	order.PurchaseType = commercedomain.NormalizeSubscriptionPurchaseType(purchaseType)
	order.GroupBuyId = groupBuyID
}

func PrepareSubscriptionPurchase(userID int, req SubscriptionPurchaseFields) (*commerceschema.SubscriptionPlan, *commercedomain.SubscriptionPurchasePreview, string, int64, error) {
	plan, err := GetSubscriptionPlanByID(req.PlanID)
	if err != nil {
		return nil, nil, "", 0, err
	}
	if !plan.Enabled {
		return nil, nil, "", 0, errors.New("plan is disabled")
	}
	if plan.InternalOnly {
		return nil, nil, "", 0, errors.New("internal plan cannot be purchased")
	}

	purchaseType, groupBuyID, err := NormalizeSubscriptionPurchaseFields(userID, req)
	if err != nil {
		return nil, nil, "", 0, err
	}
	preview, err := ResolveSubscriptionPurchasePreview(userID, plan)
	if err != nil {
		return nil, nil, "", 0, err
	}
	if preview.Action == commerceschema.SubscriptionPurchaseActionDisabled {
		return nil, nil, "", 0, errors.New(preview.DisabledReason)
	}
	if plan.MaxPurchasePerUser > 0 {
		count, err := CountUserSubscriptionsByPlan(userID, plan.Id)
		if err != nil {
			return nil, nil, "", 0, err
		}
		if count >= int64(plan.MaxPurchasePerUser) {
			return nil, nil, "", 0, errors.New("purchase limit reached")
		}
	}
	return plan, preview, purchaseType, groupBuyID, nil
}

func CreateSubscriptionEpayPayment(userID int, req SubscriptionEpayPayRequest) (*SubscriptionEpayCheckoutPayload, error) {
	if !commercestore.ContainsPayMethod(req.PaymentMethod) {
		return nil, errors.New("payment method is not available")
	}

	plan, preview, purchaseType, groupBuyID, err := PrepareSubscriptionPurchase(userID, SubscriptionPurchaseFields{
		PlanID:       req.PlanID,
		PurchaseType: req.PurchaseType,
		GroupBuyID:   req.GroupBuyID,
	})
	if err != nil {
		return nil, err
	}
	if preview.AmountDue < 0.01 {
		return nil, errors.New("plan amount is too low")
	}

	callBackAddress := CallbackAddress()
	returnURL, err := url.Parse(callBackAddress + "/api/subscription/epay/return")
	if err != nil {
		return nil, errors.New("payment callback address is invalid")
	}
	notifyURL, err := url.Parse(callBackAddress + "/api/subscription/epay/notify")
	if err != nil {
		return nil, errors.New("payment callback address is invalid")
	}

	tradeNo := fmt.Sprintf("SUBUSR%dNO%s%d", userID, platformruntime.GetRandomString(6), time.Now().Unix())
	client := GetEpayClient()
	if client == nil {
		return nil, errors.New("payment gateway is not configured")
	}

	order := &commerceschema.SubscriptionOrder{
		UserId:          userID,
		PlanId:          plan.Id,
		Money:           preview.AmountDue,
		TradeNo:         tradeNo,
		PaymentMethod:   req.PaymentMethod,
		PaymentProvider: commerceschema.PaymentProviderEpay,
		CreateTime:      time.Now().Unix(),
		Status:          constant.TopUpStatusPending,
	}
	ApplySubscriptionPurchaseFields(order, purchaseType, groupBuyID)
	if _, err := CreatePendingSubscriptionOrderWithDiscounts(order, preview.BaseAmountDue); err != nil {
		return nil, errors.New("failed to create order")
	}

	uri, params, err := client.Purchase(&epay.PurchaseArgs{
		Type:           req.PaymentMethod,
		ServiceTradeNo: tradeNo,
		Name:           fmt.Sprintf("SUB:%s", plan.Title),
		Money:          strconv.FormatFloat(order.Money, 'f', 2, 64),
		Device:         epay.PC,
		NotifyUrl:      notifyURL,
		ReturnUrl:      returnURL,
	})
	if err != nil {
		_ = ExpireSubscriptionOrder(tradeNo, commerceschema.PaymentProviderEpay)
		return nil, errors.New("failed to create payment")
	}

	return &SubscriptionEpayCheckoutPayload{
		SubscriptionCheckoutPayload: SubscriptionCheckoutPayload{
			OrderID:   tradeNo,
			AmountDue: order.Money,
			Action:    preview.Action,
		},
		Form: params,
		URL:  uri,
	}, nil
}

func VerifySubscriptionEpay(params map[string]string) (*epay.VerifyRes, error) {
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

func CompleteSubscriptionEpayPayment(verifyInfo *epay.VerifyRes) error {
	if verifyInfo == nil || verifyInfo.TradeStatus != epay.StatusTradeSuccess {
		return errors.New("payment not complete")
	}
	LockOrder(verifyInfo.ServiceTradeNo)
	defer UnlockOrder(verifyInfo.ServiceTradeNo)
	return CompleteSubscriptionOrder(verifyInfo.ServiceTradeNo, platformtext.GetJsonString(verifyInfo), commerceschema.PaymentProviderEpay, verifyInfo.Type)
}

func ResolveSubscriptionEpayReturnURL(verifyInfo *epay.VerifyRes) string {
	if verifyInfo == nil {
		return BuildPaymentReturnPath("/packages?pay=fail")
	}
	if verifyInfo.TradeStatus == epay.StatusTradeSuccess {
		if err := CompleteSubscriptionEpayPayment(verifyInfo); err == nil {
			return BuildPaymentReturnPath("/packages?pay=success")
		}
		return BuildPaymentReturnPath("/packages?pay=fail")
	}
	return BuildPaymentReturnPath("/packages?pay=pending")
}

func CreateSubscriptionXunhuPayment(userID int, req SubscriptionXunhuPayRequest) (*SubscriptionXunhuCheckoutPayload, error) {
	plan, preview, purchaseType, groupBuyID, err := PrepareSubscriptionPurchase(userID, SubscriptionPurchaseFields{
		PlanID:       req.PlanID,
		PurchaseType: req.PurchaseType,
		GroupBuyID:   req.GroupBuyID,
	})
	if err != nil {
		return nil, err
	}
	if preview.AmountDue < 0.01 {
		return nil, errors.New("plan amount is too low")
	}

	callbackAddress := CallbackAddress()
	tradeNo := fmt.Sprintf("SUBUSR%dNO%s%d", userID, platformruntime.GetRandomString(6), time.Now().Unix())
	notifyURL := callbackAddress + "/api/subscription/xunhu/notify"
	returnURL := callbackAddress + "/api/subscription/xunhu/return?trade_no=" + tradeNo

	order := &commerceschema.SubscriptionOrder{
		UserId:          userID,
		PlanId:          plan.Id,
		Money:           preview.AmountDue,
		TradeNo:         tradeNo,
		PaymentMethod:   commerceschema.PaymentMethodXunhu,
		PaymentProvider: commerceschema.PaymentProviderXunhu,
		CreateTime:      time.Now().Unix(),
		Status:          constant.TopUpStatusPending,
	}
	ApplySubscriptionPurchaseFields(order, purchaseType, groupBuyID)
	if _, err := CreatePendingSubscriptionOrderWithDiscounts(order, preview.BaseAmountDue); err != nil {
		return nil, errors.New("failed to create order")
	}

	payResult, err := CreateXunhuOrder(tradeNo, fmt.Sprintf("SUB:%s", plan.Title), order.Money, notifyURL, returnURL)
	if err != nil {
		_ = ExpireSubscriptionOrder(tradeNo, commerceschema.PaymentProviderXunhu)
		return nil, errors.New(FormatXunhuCreatePaymentError(err))
	}
	return &SubscriptionXunhuCheckoutPayload{
		SubscriptionCheckoutPayload: SubscriptionCheckoutPayload{
			OrderID:   tradeNo,
			AmountDue: order.Money,
			Action:    preview.Action,
		},
		PayURL:    payResult.PayURL,
		QRCodeURL: payResult.QRCodeURL,
	}, nil
}

func CompleteSubscriptionXunhuPayment(params map[string]string) (bool, error) {
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
	if err := CompleteSubscriptionOrder(tradeNo, platformtext.GetJsonString(params), commerceschema.PaymentProviderXunhu, commerceschema.PaymentMethodXunhu); err != nil {
		return false, err
	}
	return true, nil
}

func ResolveSubscriptionXunhuReturnURL(tradeNo string) string {
	order := GetSubscriptionOrderByTradeNo(tradeNo)
	if order != nil && order.Status == constant.TopUpStatusSuccess {
		return BuildPaymentReturnPath("/packages?pay=success")
	}
	return BuildPaymentReturnPath("/packages?pay=pending")
}

func CreateSubscriptionStripePayment(userID int, req SubscriptionStripePayRequest) (*SubscriptionStripeCheckoutPayload, error) {
	plan, preview, purchaseType, groupBuyID, err := PrepareSubscriptionPurchase(userID, SubscriptionPurchaseFields{
		PlanID:       req.PlanID,
		PurchaseType: req.PurchaseType,
		GroupBuyID:   req.GroupBuyID,
	})
	if err != nil {
		return nil, err
	}
	if preview.Action == commerceschema.SubscriptionPurchaseActionUpgrade && preview.AmountDue != plan.PriceAmount {
		return nil, errors.New("subscription upgrades are currently supported via WeChat Pay only")
	}
	if !strings.HasPrefix(commercestore.StripeApiSecret, "sk_") && !strings.HasPrefix(commercestore.StripeApiSecret, "rk_") {
		return nil, errors.New("Stripe is not configured correctly")
	}
	if commercestore.StripeWebhookSecret == "" {
		return nil, errors.New("Stripe webhook is not configured")
	}

	user, err := loadCommerceUserByID(userID, false)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("user not found")
	}

	reference := fmt.Sprintf("sub-stripe-ref-%d-%d-%s", user.Id, time.Now().UnixMilli(), randstr.String(4))
	referenceID := "sub_ref_" + platformsecurity.Sha1([]byte(reference))
	order := &commerceschema.SubscriptionOrder{
		UserId:          userID,
		PlanId:          plan.Id,
		Money:           preview.AmountDue,
		TradeNo:         referenceID,
		PaymentMethod:   commerceschema.PaymentMethodStripe,
		PaymentProvider: commerceschema.PaymentProviderStripe,
		CreateTime:      time.Now().Unix(),
		Status:          constant.TopUpStatusPending,
	}
	ApplySubscriptionPurchaseFields(order, purchaseType, groupBuyID)
	if _, err := CreatePendingSubscriptionOrderWithDiscounts(order, preview.BaseAmountDue); err != nil {
		return nil, errors.New("failed to create order")
	}
	payLink, err := genStripeSubscriptionLink(referenceID, user.StripeCustomer, user.Email, plan.Title, order.Money)
	if err != nil {
		_ = ExpireSubscriptionOrder(referenceID, commerceschema.PaymentProviderStripe)
		return nil, errors.New("failed to create payment")
	}
	return &SubscriptionStripeCheckoutPayload{
		SubscriptionCheckoutPayload: SubscriptionCheckoutPayload{
			OrderID:   referenceID,
			AmountDue: order.Money,
			Action:    preview.Action,
		},
		PayLink: payLink,
	}, nil
}

func CreateSubscriptionCreemPayment(userID int, req SubscriptionCreemPayRequest) (*SubscriptionCreemCheckoutPayload, error) {
	plan, preview, purchaseType, groupBuyID, err := PrepareSubscriptionPurchase(userID, SubscriptionPurchaseFields{
		PlanID:       req.PlanID,
		PurchaseType: req.PurchaseType,
		GroupBuyID:   req.GroupBuyID,
	})
	if err != nil {
		return nil, err
	}
	if preview.Action == commerceschema.SubscriptionPurchaseActionUpgrade && preview.AmountDue != plan.PriceAmount {
		return nil, errors.New("subscription upgrades are currently supported via WeChat Pay only")
	}
	if plan.CreemProductId == "" {
		return nil, errors.New("Creem product is not configured for this plan")
	}
	if commercestore.CreemWebhookSecret == "" && !commercestore.CreemTestMode {
		return nil, errors.New("Creem webhook is not configured")
	}

	user, err := loadCommerceUserByID(userID, false)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("user not found")
	}

	reference := "sub-creem-ref-" + randstr.String(6)
	referenceID := "sub_ref_" + platformsecurity.Sha1([]byte(reference+time.Now().String()+user.Username))
	order := &commerceschema.SubscriptionOrder{
		UserId:          userID,
		PlanId:          plan.Id,
		Money:           preview.AmountDue,
		TradeNo:         referenceID,
		PaymentMethod:   commerceschema.PaymentMethodCreem,
		PaymentProvider: commerceschema.PaymentProviderCreem,
		CreateTime:      time.Now().Unix(),
		Status:          constant.TopUpStatusPending,
	}
	ApplySubscriptionPurchaseFields(order, purchaseType, groupBuyID)
	if _, err := CreatePendingSubscriptionOrderWithDiscounts(order, preview.BaseAmountDue); err != nil {
		return nil, errors.New("failed to create order")
	}

	currency := "USD"
	switch platformgeneral.GetSetting().QuotaDisplayType {
	case platformgeneral.QuotaDisplayTypeCNY:
		currency = "CNY"
	case platformgeneral.QuotaDisplayTypeUSD:
		currency = "USD"
	}
	product := &CreemProduct{
		ProductID: plan.CreemProductId,
		Name:      plan.Title,
		Price:     order.Money,
		Currency:  currency,
		Quota:     0,
	}
	checkoutURL, err := GenCreemLink(context.Background(), referenceID, product, user.Email, user.Username, order.Money)
	if err != nil {
		_ = ExpireSubscriptionOrder(referenceID, commerceschema.PaymentProviderCreem)
		return nil, errors.New("failed to create payment")
	}
	return &SubscriptionCreemCheckoutPayload{
		SubscriptionCheckoutPayload: SubscriptionCheckoutPayload{
			OrderID:   referenceID,
			AmountDue: order.Money,
			Action:    preview.Action,
		},
		CheckoutURL: checkoutURL,
	}, nil
}

func CollectSubscriptionPaymentParams(rawURLValues url.Values) map[string]string {
	params := make(map[string]string, len(rawURLValues))
	for key := range rawURLValues {
		params[key] = rawURLValues.Get(key)
	}
	return params
}

func genStripeSubscriptionLink(referenceID string, customerID string, email string, productName string, amountDue float64) (string, error) {
	stripe.Key = commercestore.StripeApiSecret
	unitAmount := StripeMoneyToMinorUnits(amountDue)
	if unitAmount < 1 {
		return "", fmt.Errorf("invalid stripe amount")
	}
	params := &stripe.CheckoutSessionParams{
		ClientReferenceID: stripe.String(referenceID),
		SuccessURL:        stripe.String(BuildPaymentReturnPath("/packages")),
		CancelURL:         stripe.String(BuildPaymentReturnPath("/packages")),
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
		Mode: stripe.String(string(stripe.CheckoutSessionModePayment)),
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
