package app

import (
	"github.com/sh2001sh/new-api/constant"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	"strings"
	"time"

	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	identitydomain "github.com/sh2001sh/new-api/internal/identity/domain"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
)

const starterUpgradeBonusWindow = 72 * time.Hour

var starterUpgradeBonuses = map[string]int{
	"Lite月卡":     10,
	"Standard月卡": 30,
	"Pro月卡":      60,
	"Ultra月卡":    100,
}

// SubscriptionPlanDTO is the public subscription plan payload returned to commerce clients.
type SubscriptionPlanDTO struct {
	Plan                            commerceschema.SubscriptionPlan `json:"plan"`
	Action                          string                          `json:"action,omitempty"`
	BaseAmountDue                   float64                         `json:"base_amount_due,omitempty"`
	AmountDue                       float64                         `json:"amount_due,omitempty"`
	DisabledReason                  string                          `json:"disabled_reason,omitempty"`
	FirstPurchaseDiscountApplied    bool                            `json:"first_purchase_discount_applied,omitempty"`
	FirstPurchaseDiscountMultiplier float64                         `json:"first_purchase_discount_multiplier,omitempty"`
}

// UpdateSubscriptionPreferenceRequest captures user billing and subscription ordering preferences.
type UpdateSubscriptionPreferenceRequest struct {
	BillingPreference    string   `json:"billing_preference"`
	FundingSourceOrder   []string `json:"funding_source_order"`
	SubscriptionOrderIds []int    `json:"subscription_order_ids"`
}

// CreateSubscriptionClaudeConversionRequest requests monthly-pass settlement to unified credit.
type CreateSubscriptionClaudeConversionRequest struct {
	SubscriptionId    int    `json:"subscription_id"`
	ConversionPercent int    `json:"conversion_percent"`
	RequestId         string `json:"request_id"`
}

// ListSubscriptionPlans returns enabled public subscription plans with purchase previews when available.
func ListSubscriptionPlans(userID int) ([]SubscriptionPlanDTO, error) {
	if !IsPaymentComplianceConfirmed() {
		return []SubscriptionPlanDTO{}, nil
	}

	var plans []commerceschema.SubscriptionPlan
	if err := platformdb.DB.Where("enabled = ? AND internal_only = ?", true, false).
		Order("sort_order desc, id desc").
		Find(&plans).Error; err != nil {
		return nil, err
	}

	result := make([]SubscriptionPlanDTO, 0, len(plans))
	for _, plan := range plans {
		record := SubscriptionPlanDTO{Plan: plan}
		if userID > 0 {
			preview, err := ResolveSubscriptionPurchasePreview(userID, &plan)
			if err == nil && preview != nil {
				record.Action = preview.Action
				record.BaseAmountDue = preview.BaseAmountDue
				record.AmountDue = preview.AmountDue
				record.DisabledReason = preview.DisabledReason
				record.FirstPurchaseDiscountApplied = preview.FirstPurchaseDiscountApplied
				record.FirstPurchaseDiscountMultiplier = preview.FirstPurchaseDiscountMultiplier
			}
		}
		result = append(result, record)
	}
	return result, nil
}

// BuildStarterUpgradeBonusPayload returns the starter-upgrade bonus snapshot for the current user.
func BuildStarterUpgradeBonusPayload(userID int) (map[string]any, error) {
	eligible, err := HasStarterPurchaseWithin(userID, starterUpgradeBonusWindow)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"eligible":     eligible,
		"window_hours": int(starterUpgradeBonusWindow.Hours()),
		"bonuses":      cloneStarterUpgradeBonuses(),
	}, nil
}

// BuildSubscriptionOrderStatusPayload returns a user's subscription order status by trade number.
func BuildSubscriptionOrderStatusPayload(userID int, tradeNo string) (map[string]any, error) {
	order, err := GetSubscriptionOrderByTradeNoForUser(strings.TrimSpace(tradeNo), userID)
	if err != nil {
		return nil, err
	}
	planTitle := ""
	if plan, planErr := GetSubscriptionPlanByID(order.PlanId); planErr == nil && plan != nil {
		planTitle = plan.Title
	}
	payload := map[string]any{
		"trade_no":                           order.TradeNo,
		"status":                             order.Status,
		"plan_id":                            order.PlanId,
		"plan_title":                         planTitle,
		"money":                              order.Money,
		"original_money":                     order.OriginalMoney,
		"first_purchase_discount_applied":    order.FirstPurchaseDiscountApplied,
		"first_purchase_discount_multiplier": order.FirstPurchaseDiscountMultiplier,
		"payment_method":                     order.PaymentMethod,
		"payment_provider":                   order.PaymentProvider,
		"create_time":                        order.CreateTime,
		"complete_time":                      order.CompleteTime,
		"fulfillment_status":                 order.FulfillmentStatus,
		"target_subscription_id":             order.TargetSubscriptionId,
	}
	if order.Status == constant.TopUpStatusSuccess && order.FulfillmentStatus == commerceschema.SubscriptionOrderFulfillmentCompleted {
		if reveal := buildSubscriptionLuckyOrderReveal(userID, order.PlanId, order.TargetSubscriptionId); reveal != nil {
			payload["lucky_number"] = reveal["lucky_number"]
			if reveal["blind_box_benefit"] != nil {
				payload["blind_box_benefit"] = reveal["blind_box_benefit"]
			}
		}
	}
	return payload, nil
}

// buildSubscriptionLuckyOrderReveal returns only the benefit fields needed by the
// purchase confirmation UI. It intentionally avoids exposing another user's data
// when an order is renewed or upgraded without a direct subscription FK.
func buildSubscriptionLuckyOrderReveal(userID, planID, targetSubscriptionID int) map[string]any {
	if userID <= 0 || planID <= 0 || platformdb.DB == nil {
		return nil
	}
	var subscription commerceschema.UserSubscription
	query := platformdb.DB.Where("user_id = ? AND plan_id = ?", userID, planID)
	if targetSubscriptionID > 0 {
		query = query.Where("id = ?", targetSubscriptionID)
	}
	if err := query.Order("start_time desc, id desc").First(&subscription).Error; err != nil {
		return nil
	}
	var plan commerceschema.SubscriptionPlan
	if err := platformdb.DB.Where("id = ?", planID).First(&plan).Error; err != nil {
		return nil
	}
	result := map[string]any{}
	if platformdb.DB.Migrator().HasTable(&commerceschema.SubscriptionLuckyNumber{}) {
		var number commerceschema.SubscriptionLuckyNumber
		if err := platformdb.DB.Where("user_subscription_id = ?", subscription.Id).First(&number).Error; err == nil {
			result["lucky_number"] = map[string]any{
				"card_code":       number.CardCode,
				"lucky_suffix":    number.LuckySuffix,
				"membership_tier": commercedomain.NormalizeSubscriptionMembershipTier(plan.MembershipTier),
			}
		}
		if strings.TrimSpace(subscription.LuckyBenefitCycle) != "" && platformdb.DB.Migrator().HasTable(&commerceschema.SubscriptionBlindBoxBenefitCycle{}) {
			var benefit commerceschema.SubscriptionBlindBoxBenefitCycle
			if err := platformdb.DB.Where("user_subscription_id = ? AND benefit_cycle = ?", subscription.Id, subscription.LuckyBenefitCycle).First(&benefit).Error; err == nil {
				result["blind_box_benefit"] = map[string]any{
					"expected_count":  benefit.ExpectedCount,
					"granted_count":   benefit.GrantedCount,
					"membership_tier": benefit.MembershipTier,
					"starts_at":       benefit.StartsAt,
					"ends_at":         benefit.EndsAt,
					"status":          benefit.Status,
				}
			}
		}
	}
	return result
}

// BuildSubscriptionSelfPayload returns the user's subscription overview and preference state.
func BuildSubscriptionSelfPayload(userID int) (map[string]any, error) {
	settingMap, _ := identitystore.LoadUserSetting(userID, false)
	preference := commercedomain.NormalizeBillingPreference(settingMap.BillingPreference)
	fundingSourceOrder := commercedomain.NormalizeFundingSourceOrder(settingMap.FundingSourceOrder, preference)
	preference = commercedomain.BillingPreferenceFromFundingSourceOrder(fundingSourceOrder)

	allSubscriptions, err := GetAllUserSubscriptions(userID)
	if err != nil {
		allSubscriptions = []commercedomain.SubscriptionSummary{}
	}
	activeSubscriptions, err := GetAllActiveUserSubscriptions(userID)
	if err != nil {
		activeSubscriptions = []commercedomain.SubscriptionSummary{}
	}

	resetOpportunity, err := GetUserSubscriptionResetOpportunity(userID)
	if err != nil {
		return nil, err
	}
	recentConversions, err := ListRecentSubscriptionClaudeConversions(userID, 10)
	if err != nil {
		return nil, err
	}

	activeSubscriptionIDs := make([]int, 0, len(activeSubscriptions))
	activeSubscriptionSet := make(map[int]struct{}, len(activeSubscriptions))
	for _, item := range activeSubscriptions {
		if item.Subscription == nil || item.Subscription.Id <= 0 {
			continue
		}
		if plan, planErr := GetSubscriptionPlanByID(item.Subscription.PlanId); planErr == nil && plan != nil {
			if commercedomain.NormalizeSubscriptionPlanType(plan.PlanType) == commerceschema.SubscriptionPlanTypeMonthly {
				preview := BuildSubscriptionClaudeConversionPreview(plan, item.Subscription)
				if preview.Eligible {
					resetUsed, resetErr := subscriptionHasResetOpportunityUsageTx(platformdb.DB, item.Subscription.Id)
					if resetErr != nil {
						return nil, resetErr
					}
					if resetUsed {
						preview.Eligible = false
						preview.IneligibleReason = commerceschema.ErrSubscriptionClaudeConversionResetUsed.Error()
					}
				}
				item.Subscription.ConversionPreview = &preview
			}
		}
		activeSubscriptionIDs = append(activeSubscriptionIDs, item.Subscription.Id)
		activeSubscriptionSet[item.Subscription.Id] = struct{}{}
	}

	orderedIDs := make([]int, 0, len(activeSubscriptionIDs))
	for _, id := range commercedomain.NormalizePositiveIntSlice(settingMap.SubscriptionOrderIds) {
		if _, ok := activeSubscriptionSet[id]; !ok {
			continue
		}
		orderedIDs = append(orderedIDs, id)
		delete(activeSubscriptionSet, id)
	}
	for _, id := range activeSubscriptionIDs {
		if _, ok := activeSubscriptionSet[id]; !ok {
			continue
		}
		orderedIDs = append(orderedIDs, id)
		delete(activeSubscriptionSet, id)
	}

	return map[string]any{
		"billing_preference":     preference,
		"funding_source_order":   fundingSourceOrder,
		"subscription_order_ids": orderedIDs,
		"subscriptions":          activeSubscriptions,
		"all_subscriptions":      allSubscriptions,
		"reset_opportunity":      resetOpportunity,
		"conversion_config":      GetSubscriptionClaudeConversionConfig(),
		"recent_conversions":     recentConversions,
	}, nil
}

// UseSubscriptionResetOpportunity resets the current user's active subscription usage.
func UseSubscriptionResetOpportunity(userID int) (map[string]any, error) {
	result, err := UseUserSubscriptionResetOpportunity(userID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"reset_opportunity":   result.ResetOpportunity,
		"subscription_id":     result.UserSubscriptionId,
		"amount_used_before":  result.AmountUsedBefore,
		"amount_used_after":   result.AmountUsedAfter,
		"period_used_before":  result.PeriodUsedBefore,
		"period_used_after":   result.PeriodUsedAfter,
		"cleared_used_amount": result.ClearedUsedAmount,
	}, nil
}

// BuildSubscriptionClaudeConversionsPayload returns recent conversion records and the active config.
func BuildSubscriptionClaudeConversionsPayload(userID int) (map[string]any, error) {
	items, err := ListRecentSubscriptionClaudeConversions(userID, 20)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"items":  items,
		"config": GetSubscriptionClaudeConversionConfig(),
	}, nil
}

// CreateSubscriptionClaudeConversion converts subscription quota into Claude quota for the current user.
func CreateSubscriptionClaudeConversion(userID int, req CreateSubscriptionClaudeConversionRequest) (map[string]any, error) {
	result, err := ConvertMonthlyPassToUnifiedCredit(req.RequestId, userID, req.SubscriptionId, req.ConversionPercent)
	if err != nil {
		return nil, err
	}
	if planInfo, planErr := GetSubscriptionPlanInfoByUserSubscriptionID(req.SubscriptionId); planErr == nil && planInfo != nil {
		auditapp.RecordLog(userID, auditschema.LogTypeTopup, BuildSubscriptionClaudeConversionLog(planInfo.PlanTitle, result.ConversionPercent, result.TargetQuota))
	}
	return map[string]any{
		"subscription_id":       result.SubscriptionId,
		"source_quota":          result.SourceQuota,
		"target_quota":          result.TargetQuota,
		"quota_after":           result.QuotaAfter,
		"amount_used_after":     result.AmountUsedAfter,
		"period_used_after":     result.PeriodUsedAfter,
		"plan_price_amount":     result.PlanPriceAmount,
		"unused_ratio":          result.UnusedRatio,
		"conversion_percent":    result.ConversionPercent,
		"remaining_ratio_after": result.RemainingRatioAfter,
		"subscription_ended":    result.SubscriptionEnded,
		"conversion":            result.Conversion,
		"config":                result.Config,
	}, nil
}

// UpdateSubscriptionPreference persists the user's billing preference and active subscription ordering.
func UpdateSubscriptionPreference(userID int, req UpdateSubscriptionPreferenceRequest) (map[string]any, error) {
	preference := commercedomain.NormalizeBillingPreference(req.BillingPreference)
	fundingSourceOrder := commercedomain.NormalizeFundingSourceOrder(req.FundingSourceOrder, preference)
	preference = commercedomain.BillingPreferenceFromFundingSourceOrder(fundingSourceOrder)
	orderIDs := commercedomain.NormalizePositiveIntSlice(req.SubscriptionOrderIds)

	user, err := loadCommerceUserByID(userID, true)
	if err != nil {
		return nil, err
	}
	current := identitydomain.GetSetting(user)
	current.BillingPreference = preference
	current.FundingSourceOrder = fundingSourceOrder
	if req.SubscriptionOrderIds != nil {
		current.SubscriptionOrderIds = orderIDs
	}
	identitydomain.SetSetting(user, current)
	if err := identitystore.UpdateUser(user, false); err != nil {
		return nil, err
	}

	return map[string]any{
		"billing_preference":     preference,
		"funding_source_order":   current.FundingSourceOrder,
		"subscription_order_ids": current.SubscriptionOrderIds,
	}, nil
}

func cloneStarterUpgradeBonuses() map[string]int {
	cloned := make(map[string]int, len(starterUpgradeBonuses))
	for key, value := range starterUpgradeBonuses {
		cloned[key] = value
	}
	return cloned
}
