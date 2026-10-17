package app

import (
	"errors"
	"fmt"

	"github.com/sh2001sh/new-api/constant"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

const creditPackChannelShareBPS int64 = 6000

func validCreditPackPurchaseQuota(granted, paid int64) bool {
	if granted <= 0 || paid <= 0 {
		return false
	}
	value := decimal.NewFromInt(granted).Mul(decimal.NewFromInt(100))
	basis := decimal.NewFromInt(paid)
	return value.GreaterThanOrEqual(basis.Mul(decimal.NewFromInt(105))) && value.LessThanOrEqual(basis.Mul(decimal.NewFromInt(110)))
}

// Credit packs retain a separate permanent account so a channel's opt-in and
// promotional cost can be enforced without changing ordinary wallet funds.
func fulfillCreditPackTx(tx *gorm.DB, order *commerceschema.SubscriptionOrder) error {
	if !validCreditPackPurchaseQuota(order.CreditPackQuota, order.CreditPackPaidQuota) || order.Money <= 0 || order.CreditPackChannelShareBPS != creditPackChannelShareBPS {
		return errors.New("invalid credit pack quota")
	}
	if order.TargetSubscriptionId == 0 {
		sub, err := createCreditPackBalanceTx(tx, order.UserId, order.PlanId, order.CreditPackQuota, order.CreditPackPaidQuota, order.CreditPackChannelShareBPS, "credit-pack:"+order.TradeNo)
		if err != nil {
			return err
		}
		order.TargetSubscriptionId = sub.Id
	}
	order.FulfillmentStatus = commerceschema.SubscriptionOrderFulfillmentCompleted
	return tx.Save(order).Error
}

func createCreditPackBalanceTx(tx *gorm.DB, userID, planID int, granted, paid, channelShareBPS int64, reference string) (*commerceschema.UserSubscription, error) {
	return createCreditPackBalanceWithReasonTx(tx, userID, planID, granted, paid, channelShareBPS, reference, "credit_pack_purchase")
}

func createCreditPackBalanceWithReasonTx(tx *gorm.DB, userID, planID int, granted, paid, channelShareBPS int64, reference, reason string) (*commerceschema.UserSubscription, error) {
	if granted <= 0 || paid <= 0 || paid > granted || channelShareBPS < 0 || channelShareBPS > 10000 {
		return nil, errors.New("invalid credit pack balance")
	}
	sub := &commerceschema.UserSubscription{
		UserId: userID, PlanId: planID, AmountTotal: granted,
		CreditPackGrantedQuota: granted, CreditPackPaidQuota: paid, CreditPackChannelShareBPS: channelShareBPS,
		StartTime: platformruntime.GetTimestamp(), EndTime: 0, Status: "active", Source: "credit_pack",
	}
	if err := tx.Create(sub).Error; err != nil {
		return nil, err
	}
	account, err := billingdomain.EnsureBillingAccountTx(tx, billingdomain.EnsureAccountParams{AccountType: "subscription", OwnerType: "user_subscription", OwnerID: int64(sub.Id), QuotaUnit: "quota"})
	if err != nil {
		return nil, err
	}
	_, err = billingdomain.CreditAccountTx(tx, billingdomain.CreditAccountParams{
		AccountID: account.AccountID, Amount: granted, IdempotencyKey: reference,
		ReasonCode: reason, ReferenceType: "user_subscription", ReferenceID: fmt.Sprint(sub.Id), OperatorType: "commerce",
	})
	return sub, err
}

// Conversion balances can reference an original plan. Their immutable source
// takes priority over any later catalog edits, legacy resets, or model limits.
func creditPackSnapshotPlan(plan *commerceschema.SubscriptionPlan, sub *commerceschema.UserSubscription) *commerceschema.SubscriptionPlan {
	if plan == nil || sub == nil || sub.CreditPackGrantedQuota <= 0 {
		return plan
	}
	copy := *plan
	copy.PlanType = commerceschema.SubscriptionPlanTypeCreditPack
	copy.DurationUnit, copy.DurationValue = commerceschema.SubscriptionDurationPermanent, 1
	copy.QuotaResetPeriod, copy.QuotaResetCustomSeconds = commerceschema.SubscriptionResetNever, 0
	copy.TotalAmount, copy.PeriodAmount, copy.ModelLimits = sub.CreditPackGrantedQuota, 0, ""
	copy.UpgradeGroup = ""
	return &copy
}

// Batch-load the exact quota, including fractions, for existing order-history views.
func attachCreditPackTopupQuota(topups []*commerceschema.TopUp) error {
	if len(topups) == 0 {
		return nil
	}
	trades := make([]string, 0, len(topups))
	for _, topup := range topups {
		trades = append(trades, topup.TradeNo)
	}
	var packs []commerceschema.SubscriptionOrder
	if err := platformdb.DB.Select("trade_no", "credit_pack_quota").Where("trade_no IN ? AND credit_pack_quota > 0", trades).Find(&packs).Error; err != nil {
		return err
	}
	quotas := make(map[string]int64, len(packs))
	for _, pack := range packs {
		quotas[pack.TradeNo] = pack.CreditPackQuota
	}
	for _, topup := range topups {
		topup.CreditedQuota = quotas[topup.TradeNo]
	}
	return nil
}

// Multiple historical payments do not store a complete price/carry-over snapshot;
// an upgrade's last payment can be only its difference. Fail visibly instead of
// mistaking that difference for the entire pass's value.
func legacyConversionPaidPlanTx(tx *gorm.DB, plan *commerceschema.SubscriptionPlan, sub *commerceschema.UserSubscription) (*commerceschema.SubscriptionPlan, error) {
	if plan == nil || sub == nil || tx == nil {
		return plan, nil
	}
	var orders []commerceschema.SubscriptionOrder
	err := tx.Where("target_subscription_id = ? AND user_id = ? AND status = ? AND purchase_type <> ?", sub.Id, sub.UserId, constant.TopUpStatusSuccess, commerceschema.SubscriptionPurchaseTypeFuel).
		Order("complete_time desc, id desc").Limit(2).Find(&orders).Error
	if err != nil {
		return nil, err
	}
	if len(orders) == 0 {
		return plan, nil
	}
	if len(orders) > 1 || orders[0].Money <= 0 {
		return nil, errors.New("历史续费或升级补差金额需核定后转换")
	}
	copy := *plan
	copy.PriceAmount = orders[0].Money
	return &copy, nil
}
