package app

import (
	"fmt"
	"github.com/sh2001sh/new-api/constant"
	blindboxsettings "github.com/sh2001sh/new-api/internal/commerce/blindboxsettings"
	commercestore "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
)

type BlindBoxAmountRequest struct {
	Quantity int `json:"quantity"`
}

type BlindBoxPayRequest struct {
	Quantity      int    `json:"quantity"`
	PaymentMethod string `json:"payment_method"`
}

type BlindBoxOpenRequest struct {
	Count int `json:"count"`
}

// BuildBlindBoxSelfPayload assembles the blind-box overview payload for the current user.
func BuildBlindBoxSelfPayload(userID int) (map[string]any, error) {
	setting := blindboxsettings.Get()
	enabled := IsPaymentComplianceConfirmed() && setting.Enabled
	overview, err := GetBlindBoxOverview(userID, 20)
	if err != nil {
		return nil, err
	}
	firstPurchaseEligible, err := IsBlindBoxFirstPurchaseEligible(userID)
	if err != nil {
		return nil, err
	}
	props, err := ListUserBlindBoxProps(userID)
	if err != nil {
		return nil, err
	}
	zeroHour, err := BuildZeroHourOverview(userID)
	if err != nil {
		return nil, err
	}
	statistics, err := GetBlindBoxStatistics(userID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"enabled":                           enabled,
		"unit_price":                        setting.UnitPrice,
		"daily_limit":                       setting.DailyLimit,
		"monthly_limit":                     setting.MonthlyLimit,
		"daily_open_limit":                  setting.DailyOpenLimit,
		"first_purchase_guarantee_usd":      setting.FirstPurchaseGuaranteeUSD,
		"first_purchase_guarantee_eligible": firstPurchaseEligible,
		"count_options":                     setting.CountOptions,
		"tiers":                             setting.Tiers,
		"subscription_prize_probability":    setting.SubscriptionPrizeProbability,
		"subscription_plan_title":           setting.SubscriptionPlanTitle,
		"pity_threshold":                    setting.PityThreshold,
		"pity_guarantee_usd":                setting.PityGuaranteeUSD,
		"low_reward_threshold_usd":          setting.LowRewardThresholdUSD,
		"pay_methods":                       buildBlindBoxPayMethods(),
		"overview":                          overview,
		"props":                             props,
		"zero_hour":                         zeroHour,
		"statistics":                        statistics,
	}, nil
}

// BuildBlindBoxAdminOverviewPayload assembles the admin blind-box overview payload for a user.
func BuildBlindBoxAdminOverviewPayload(userID int) (map[string]any, error) {
	setting := blindboxsettings.Get()
	enabled := IsPaymentComplianceConfirmed() && setting.Enabled
	overview, err := GetBlindBoxOverview(userID, 20)
	if err != nil {
		return nil, err
	}
	firstPurchaseEligible, err := IsBlindBoxFirstPurchaseEligible(userID)
	if err != nil {
		return nil, err
	}
	props, err := ListUserBlindBoxProps(userID)
	if err != nil {
		return nil, err
	}
	grants, err := ListUserBlindBoxGrants(userID, 20)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"enabled":                           enabled,
		"unit_price":                        setting.UnitPrice,
		"daily_limit":                       setting.DailyLimit,
		"monthly_limit":                     setting.MonthlyLimit,
		"daily_open_limit":                  setting.DailyOpenLimit,
		"first_purchase_guarantee_usd":      setting.FirstPurchaseGuaranteeUSD,
		"first_purchase_guarantee_eligible": firstPurchaseEligible,
		"count_options":                     setting.CountOptions,
		"tiers":                             setting.Tiers,
		"subscription_prize_probability":    setting.SubscriptionPrizeProbability,
		"subscription_plan_title":           setting.SubscriptionPlanTitle,
		"pity_threshold":                    setting.PityThreshold,
		"pity_guarantee_usd":                setting.PityGuaranteeUSD,
		"low_reward_threshold_usd":          setting.LowRewardThresholdUSD,
		"overview":                          overview,
		"props":                             props,
		"grants":                            grants,
	}, nil
}

// BuildBlindBoxOrderStatusPayload returns the blind-box order status response payload.
func BuildBlindBoxOrderStatusPayload(userID int, tradeNo string) (map[string]any, error) {
	order, err := GetBlindBoxOrderByTradeNoForUser(tradeNo, userID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"trade_no":         order.TradeNo,
		"status":           order.Status,
		"quantity":         order.Quantity,
		"opened_count":     order.OpenedCount,
		"money":            order.Money,
		"payment_method":   order.PaymentMethod,
		"payment_provider": order.PaymentProvider,
		"create_time":      order.CreateTime,
		"complete_time":    order.CompleteTime,
	}, nil
}

// QuoteBlindBoxPurchase formats the payable blind-box amount for the requested quantity.
func QuoteBlindBoxPurchase(userID int, quantity int) (string, error) {
	amount, err := ValidateBlindBoxPurchase(userID, quantity)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%.2f", amount), nil
}

// BuildBlindBoxOpenPayload opens blind boxes for the user and returns the response payload.
func BuildBlindBoxOpenPayload(userID int, count int) (map[string]any, error) {
	records, err := OpenBlindBoxes(userID, count)
	if err != nil {
		return nil, err
	}
	overview, err := GetBlindBoxOverview(userID, 20)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"records":    records,
		"overview":   overview,
		"open_count": count,
	}, nil
}

func buildBlindBoxPayMethods() []map[string]string {
	if !IsPaymentComplianceConfirmed() {
		return []map[string]string{}
	}
	setting := blindboxsettings.Get()
	if !setting.Enabled {
		return []map[string]string{}
	}
	payMethods := CloneDisplayedPayMethods(commercestore.PayMethods, "")
	filtered := make([]map[string]string, 0, len(payMethods))
	for _, method := range payMethods {
		if method["type"] == commerceschema.PaymentMethodXunhu {
			continue
		}
		method["min_topup"] = "1"
		filtered = append(filtered, method)
	}
	if IsXunhuTopUpEnabled() {
		filtered = CloneDisplayedPayMethods(filtered, "wxpay")
		filtered = append(filtered, map[string]string{
			"name":      "微信支付",
			"type":      commerceschema.PaymentMethodXunhu,
			"color":     "rgba(var(--semi-orange-5), 1)",
			"min_topup": "1",
		})
	}
	return filtered
}

func blindBoxPendingReturnURL() string {
	return BuildPaymentReturnPath("/blind-box?pay=pending")
}

func blindBoxFailedReturnURL() string {
	return BuildPaymentReturnPath("/blind-box?pay=fail")
}

func blindBoxSuccessReturnURL() string {
	return BuildPaymentReturnPath("/blind-box?pay=success")
}

func completeBlindBoxOrder(tradeNo string, providerPayload string, expectedProvider string, actualPaymentMethod string) error {
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)
	return CompleteBlindBoxOrder(tradeNo, providerPayload, expectedProvider, actualPaymentMethod)
}

func resolveBlindBoxXunhuReturnURL(tradeNo string) string {
	order := GetBlindBoxOrderByTradeNo(tradeNo)
	if order != nil && order.Status == constant.TopUpStatusSuccess {
		return blindBoxSuccessReturnURL()
	}
	return blindBoxPendingReturnURL()
}
