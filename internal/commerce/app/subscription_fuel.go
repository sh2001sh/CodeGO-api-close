package app

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/thanhpk/randstr"
	"gorm.io/gorm"

	"github.com/sh2001sh/new-api/constant"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	commercestore "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
)

var ErrSubscriptionFuelUnavailable = errors.New("subscription fuel is unavailable")

type SubscriptionFuelQuoteRequest struct {
	SubscriptionID int   `json:"subscription_id"`
	Quota          int64 `json:"quota"`
}

type SubscriptionFuelPurchaseRequest struct {
	SubscriptionFuelQuoteRequest
	PaymentMethod string `json:"payment_method"`
}

type SubscriptionFuelQuote struct {
	SubscriptionID int     `json:"subscription_id"`
	PlanID         int     `json:"plan_id"`
	PlanTitle      string  `json:"plan_title"`
	Quota          int64   `json:"quota"`
	UnitPrice      float64 `json:"unit_price"`
	AmountDue      float64 `json:"amount_due"`
	ExpiresAt      int64   `json:"expires_at"`
	MinQuota       int64   `json:"min_quota"`
	QuotaStep      int64   `json:"quota_step"`
	QuotaPerUnit   int64   `json:"quota_per_unit"`
}

func QuoteSubscriptionFuel(userID int, req SubscriptionFuelQuoteRequest) (*SubscriptionFuelQuote, error) {
	if userID <= 0 || req.SubscriptionID <= 0 || req.Quota <= 0 {
		return nil, ErrSubscriptionFuelUnavailable
	}
	var subscription commerceschema.UserSubscription
	if err := platformdb.DB.Where("id = ? AND user_id = ?", req.SubscriptionID, userID).First(&subscription).Error; err != nil {
		return nil, ErrSubscriptionFuelUnavailable
	}
	now := platformruntime.GetTimestamp()
	if subscription.Status != "active" || subscription.EndTime <= now {
		return nil, ErrSubscriptionFuelUnavailable
	}
	plan, err := GetSubscriptionPlanByID(subscription.PlanId)
	if err != nil || plan == nil || !isMonthlySubscriptionPlan(plan) || !plan.FuelEnabled {
		return nil, ErrSubscriptionFuelUnavailable
	}
	if plan.FuelUnitPrice <= 0 || plan.FuelMinQuota <= 0 || plan.FuelQuotaStep <= 0 || req.Quota < plan.FuelMinQuota || req.Quota%plan.FuelQuotaStep != 0 {
		return nil, ErrSubscriptionFuelUnavailable
	}
	quotaPerUnit := int64(platformruntime.QuotaPerUnit)
	amountDue := math.Round((float64(req.Quota)/float64(quotaPerUnit))*plan.FuelUnitPrice*100) / 100
	if amountDue < 0.01 {
		return nil, ErrSubscriptionFuelUnavailable
	}
	return &SubscriptionFuelQuote{
		SubscriptionID: subscription.Id,
		PlanID:         plan.Id,
		PlanTitle:      plan.Title,
		Quota:          req.Quota,
		UnitPrice:      plan.FuelUnitPrice,
		AmountDue:      amountDue,
		ExpiresAt:      subscription.EndTime,
		MinQuota:       plan.FuelMinQuota,
		QuotaStep:      plan.FuelQuotaStep,
		QuotaPerUnit:   quotaPerUnit,
	}, nil
}

func createPendingSubscriptionFuelOrder(userID int, quote *SubscriptionFuelQuote, method, provider, tradeNo string) (*commerceschema.SubscriptionOrder, error) {
	if quote == nil {
		return nil, ErrSubscriptionFuelUnavailable
	}
	order := &commerceschema.SubscriptionOrder{
		UserId:               userID,
		PlanId:               quote.PlanID,
		Money:                quote.AmountDue,
		TradeNo:              tradeNo,
		PaymentMethod:        method,
		PaymentProvider:      provider,
		PurchaseType:         commerceschema.SubscriptionPurchaseTypeFuel,
		TargetSubscriptionId: quote.SubscriptionID,
		FuelQuota:            quote.Quota,
		FuelUnitPrice:        quote.UnitPrice,
		FuelExpiresAt:        quote.ExpiresAt,
		Status:               constant.TopUpStatusPending,
		FulfillmentStatus:    commerceschema.SubscriptionOrderFulfillmentPending,
		CreateTime:           time.Now().Unix(),
	}
	if err := platformdb.DB.Create(order).Error; err != nil {
		return nil, err
	}
	return order, nil
}

func CreateSubscriptionFuelEpayPayment(userID int, req SubscriptionFuelPurchaseRequest) (*SubscriptionEpayCheckoutPayload, error) {
	if !commercestore.ContainsPayMethod(req.PaymentMethod) {
		return nil, errors.New("payment method is not available")
	}
	quote, err := QuoteSubscriptionFuel(userID, req.SubscriptionFuelQuoteRequest)
	if err != nil {
		return nil, err
	}
	callbackAddress := CallbackAddress()
	returnURL, err := url.Parse(callbackAddress + "/api/subscription/epay/return")
	if err != nil {
		return nil, errors.New("payment callback address is invalid")
	}
	notifyURL, err := url.Parse(callbackAddress + "/api/subscription/epay/notify")
	if err != nil {
		return nil, errors.New("payment callback address is invalid")
	}
	tradeNo := fmt.Sprintf("SUBFUEL%dNO%s%d", userID, platformruntime.GetRandomString(6), time.Now().Unix())
	order, err := createPendingSubscriptionFuelOrder(userID, quote, req.PaymentMethod, commerceschema.PaymentProviderEpay, tradeNo)
	if err != nil {
		return nil, err
	}
	client := GetEpayClient()
	if client == nil {
		return nil, errors.New("payment gateway is not configured")
	}
	uri, params, err := client.Purchase(&epay.PurchaseArgs{Type: req.PaymentMethod, ServiceTradeNo: tradeNo, Name: "FUEL:" + quote.PlanTitle, Money: strconv.FormatFloat(order.Money, 'f', 2, 64), Device: epay.PC, NotifyUrl: notifyURL, ReturnUrl: returnURL})
	if err != nil {
		_ = ExpireSubscriptionOrder(tradeNo, commerceschema.PaymentProviderEpay)
		return nil, errors.New("failed to create payment")
	}
	return &SubscriptionEpayCheckoutPayload{SubscriptionCheckoutPayload: SubscriptionCheckoutPayload{OrderID: tradeNo, AmountDue: order.Money, Action: "fuel"}, Form: params, URL: uri}, nil
}

func CreateSubscriptionFuelXunhuPayment(userID int, req SubscriptionFuelPurchaseRequest) (*SubscriptionXunhuCheckoutPayload, error) {
	quote, err := QuoteSubscriptionFuel(userID, req.SubscriptionFuelQuoteRequest)
	if err != nil {
		return nil, err
	}
	if commercestore.XunhuMinTopUp > 0 && quote.AmountDue < float64(commercestore.XunhuMinTopUp) {
		return nil, fmt.Errorf("minimum XunhuPay payment is %d CNY", commercestore.XunhuMinTopUp)
	}
	tradeNo := fmt.Sprintf("SUBFUEL%dNO%s%d", userID, platformruntime.GetRandomString(6), time.Now().Unix())
	order, err := createPendingSubscriptionFuelOrder(userID, quote, commerceschema.PaymentMethodXunhu, commerceschema.PaymentProviderXunhu, tradeNo)
	if err != nil {
		return nil, err
	}
	callbackAddress := CallbackAddress()
	payment, err := CreateXunhuOrder(tradeNo, "FUEL:"+quote.PlanTitle, order.Money, callbackAddress+"/api/subscription/xunhu/notify", callbackAddress+"/api/subscription/xunhu/return?trade_no="+tradeNo)
	if err != nil {
		_ = ExpireSubscriptionOrder(tradeNo, commerceschema.PaymentProviderXunhu)
		return nil, errors.New(FormatXunhuCreatePaymentError(err))
	}
	return &SubscriptionXunhuCheckoutPayload{SubscriptionCheckoutPayload: SubscriptionCheckoutPayload{OrderID: tradeNo, AmountDue: order.Money, Action: "fuel"}, PayURL: payment.PayURL, QRCodeURL: payment.QRCodeURL}, nil
}

func CreateSubscriptionFuelStripePayment(userID int, req SubscriptionFuelPurchaseRequest) (*SubscriptionStripeCheckoutPayload, error) {
	quote, err := QuoteSubscriptionFuel(userID, req.SubscriptionFuelQuoteRequest)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(commercestore.StripeApiSecret, "sk_") && !strings.HasPrefix(commercestore.StripeApiSecret, "rk_") {
		return nil, errors.New("Stripe is not configured correctly")
	}
	user, err := loadCommerceUserByID(userID, false)
	if err != nil || user == nil {
		return nil, errors.New("user not found")
	}
	referenceID := "sub_fuel_ref_" + platformsecurity.Sha1([]byte(fmt.Sprintf("%d-%d-%s", userID, time.Now().UnixMilli(), randstr.String(4))))
	order, err := createPendingSubscriptionFuelOrder(userID, quote, commerceschema.PaymentMethodStripe, commerceschema.PaymentProviderStripe, referenceID)
	if err != nil {
		return nil, err
	}
	link, err := genStripeSubscriptionLink(referenceID, user.StripeCustomer, user.Email, "FUEL:"+quote.PlanTitle, order.Money)
	if err != nil {
		_ = ExpireSubscriptionOrder(referenceID, commerceschema.PaymentProviderStripe)
		return nil, errors.New("failed to create payment")
	}
	return &SubscriptionStripeCheckoutPayload{SubscriptionCheckoutPayload: SubscriptionCheckoutPayload{OrderID: referenceID, AmountDue: order.Money, Action: "fuel"}, PayLink: link}, nil
}

func fulfillSubscriptionFuelTx(tx *gorm.DB, order *commerceschema.SubscriptionOrder) error {
	if tx == nil || order == nil || order.PurchaseType != commerceschema.SubscriptionPurchaseTypeFuel || order.TargetSubscriptionId <= 0 || order.FuelQuota <= 0 {
		return ErrSubscriptionFuelUnavailable
	}
	var subscription commerceschema.UserSubscription
	if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("id = ? AND user_id = ?", order.TargetSubscriptionId, order.UserId).First(&subscription).Error; err != nil {
		return ErrSubscriptionFuelUnavailable
	}
	if subscription.Status != "active" || subscription.EndTime <= platformruntime.GetTimestamp() || subscription.EndTime != order.FuelExpiresAt {
		return ErrSubscriptionFuelUnavailable
	}
	account, err := billingdomain.EnsureBillingAccountTx(tx, billingdomain.EnsureAccountParams{AccountType: "subscription", OwnerType: "user_subscription", OwnerID: int64(subscription.Id), QuotaUnit: "quota"})
	if err != nil {
		return err
	}
	if _, err = billingdomain.CreditAccountTx(tx, billingdomain.CreditAccountParams{AccountID: account.AccountID, Amount: order.FuelQuota, IdempotencyKey: "subscription-fuel:" + order.TradeNo, ReasonCode: "subscription_fuel", ReferenceType: "subscription_fuel_order", ReferenceID: order.TradeNo, OperatorType: "commerce", OperatorID: fmt.Sprintf("%d", order.Id)}); err != nil {
		return err
	}
	return tx.Model(&commerceschema.UserSubscription{}).Where("id = ?", subscription.Id).Updates(map[string]any{"amount_total": gorm.Expr("amount_total + ?", order.FuelQuota)}).Error
}
