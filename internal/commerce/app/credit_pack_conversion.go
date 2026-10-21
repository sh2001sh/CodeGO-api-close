package app

import (
	"errors"
	"math"

	"github.com/sh2001sh/new-api/constant"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// New packages exchange a percentage of the current remainder at the immutable
// paid/granted ratio. Promotions cannot become wallet principal through conversion.
func calculateCreditPackConversionQuote(sub *commerceschema.UserSubscription, percent int) monthlyPassConversionQuote {
	if sub == nil || sub.Status != "active" || sub.EndTime != 0 || sub.Source != "credit_pack" ||
		sub.CreditPackGrantedQuota <= 0 || sub.CreditPackPaidQuota <= 0 || sub.CreditPackPaidQuota > sub.CreditPackGrantedQuota ||
		sub.AmountTotal != sub.CreditPackGrantedQuota || sub.AmountUsed < 0 || sub.AmountUsed >= sub.AmountTotal {
		return monthlyPassConversionQuote{}
	}
	if percent == 0 {
		percent = 100 // Preview all remaining quota.
	}
	if percent < 1 || percent > 100 {
		return monthlyPassConversionQuote{}
	}
	remaining := sub.AmountTotal - sub.AmountUsed
	source := decimal.NewFromInt(remaining).Mul(decimal.NewFromInt(int64(percent))).Div(decimal.NewFromInt(100)).Floor().IntPart()
	// Use integer division rather than rounded decimal division for money.
	quotient, _ := decimal.NewFromInt(source).Mul(decimal.NewFromInt(sub.CreditPackPaidQuota)).QuoRem(decimal.NewFromInt(sub.CreditPackGrantedQuota), 0)
	target := quotient.IntPart()
	if source <= 0 || target <= 0 || target > math.MaxInt32 {
		return monthlyPassConversionQuote{}
	}
	return monthlyPassConversionQuote{
		remainingQuota: source, targetQuota: int(target), baseQuota: int(target),
		planPriceAmount:   decimal.NewFromInt(sub.CreditPackPaidQuota).Div(decimal.NewFromFloat(platformruntime.QuotaPerUnit)).InexactFloat64(),
		unusedRatio:       decimal.NewFromInt(remaining).Div(decimal.NewFromInt(sub.AmountTotal)).InexactFloat64(),
		conversionPercent: percent, maxConversionPercent: 100,
	}
}

func creditPackConversionOrderTx(tx *gorm.DB, sub *commerceschema.UserSubscription) error {
	var orders []commerceschema.SubscriptionOrder
	// Refunds lock order then subscription. Do not take an order lock while
	// holding the subscription: status and the native hold serialize both paths.
	if err := tx.Where("target_subscription_id = ?", sub.Id).Limit(2).Find(&orders).Error; err != nil {
		return err
	}
	if len(orders) != 1 {
		return errors.New("额度包缺少唯一有效付款订单，暂不可转换")
	}
	order := orders[0]
	if order.UserId != sub.UserId || order.PlanId != sub.PlanId || order.Status != constant.TopUpStatusSuccess ||
		order.FulfillmentStatus != commerceschema.SubscriptionOrderFulfillmentCompleted || order.PurchaseType != commerceschema.SubscriptionPurchaseTypeNormal ||
		math.IsNaN(order.Money) || math.IsInf(order.Money, 0) || order.Money <= 0 ||
		order.CreditPackQuota != sub.CreditPackGrantedQuota || order.CreditPackPaidQuota != sub.CreditPackPaidQuota ||
		quotaUnitsFromUSD(order.Money) != sub.CreditPackPaidQuota {
		return errors.New("额度包付款快照不一致，暂不可转换")
	}
	if order.RefundStatus != "" || order.RefundNo != "" || order.RefundProviderID != "" || order.RefundQuota != 0 || order.RefundAmount != 0 {
		return errors.New("额度包存在退款记录，暂不可转换")
	}
	return nil
}

func lockCreditPackConversionBalanceTx(tx *gorm.DB, sub *commerceschema.UserSubscription) error {
	var account billingschema.BillingAccount
	if err := tx.Where("account_type = ? AND owner_type = ? AND owner_id = ?", "subscription", "user_subscription", sub.Id).First(&account).Error; err != nil {
		return err
	}
	var balance billingschema.BillingBalanceSnapshot
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("account_id = ?", account.AccountID).First(&balance).Error; err != nil {
		return err
	}
	var open int64
	if err := tx.Model(&billingschema.BillingReservation{}).Where("account_id = ? AND status = ?", account.AccountID, billingschema.BillingReservationStatusOpen).Count(&open).Error; err != nil {
		return err
	}
	if open > 0 || balance.ReservedBalance != 0 {
		return commerceschema.ErrSubscriptionClaudeConversionInProgress
	}
	if balance.AvailableBalance != sub.AmountTotal-sub.AmountUsed {
		return errors.New("额度包账务尚未同步，请稍后重试")
	}
	return nil
}
