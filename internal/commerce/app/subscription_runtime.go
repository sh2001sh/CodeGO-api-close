package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/sh2001sh/new-api/constant"
	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"math"
	"strings"
	"time"

	// ResolveSubscriptionPurchasePreview computes the current purchase action and payable amount.
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	defaultPendingSubscriptionOrderExpiryMinutes = 30
	minimumPendingSubscriptionOrderExpiryMinutes = 3
	maximumPendingSubscriptionOrderExpiryMinutes = 24 * 60
)

// PendingSubscriptionOrderExpiry is the longest an unpaid checkout may keep
// a subscription purchase or discount card reserved.
func PendingSubscriptionOrderExpiry() time.Duration {
	minutes := platformconfig.GetEnvOrDefaultInt("SUBSCRIPTION_ORDER_PENDING_EXPIRY_MINUTES", defaultPendingSubscriptionOrderExpiryMinutes)
	if minutes < minimumPendingSubscriptionOrderExpiryMinutes || minutes > maximumPendingSubscriptionOrderExpiryMinutes {
		minutes = defaultPendingSubscriptionOrderExpiryMinutes
	}
	return time.Duration(minutes) * time.Minute
}

func ResolveSubscriptionPurchasePreview(userID int, targetPlan *commerceschema.SubscriptionPlan) (*commercedomain.SubscriptionPurchasePreview, error) {
	return resolveSubscriptionPurchasePreviewTx(nil, userID, targetPlan)
}

// CreatePendingSubscriptionOrderWithDiscounts creates a pending order and atomically reserves its discount.
// The first-plan campaign takes precedence so a blind-box prop is not consumed by the same order.
func CreatePendingSubscriptionOrderWithDiscounts(order *commerceschema.SubscriptionOrder, baseMoney float64) (float64, error) {
	if order == nil {
		return 0, errors.New("order is nil")
	}
	if order.UserId <= 0 || strings.TrimSpace(order.TradeNo) == "" {
		return 0, errors.New("invalid subscription order")
	}
	if baseMoney <= 0 {
		baseMoney = order.Money
	}

	appliedRate := 0.0
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		order.FulfillmentStatus = commerceschema.SubscriptionOrderFulfillmentPending
		plan, err := getSubscriptionPlanRecordTx(tx, order.PlanId)
		if err != nil {
			return err
		}
		if plan.PlanType == commerceschema.SubscriptionPlanTypeCreditPack {
			if !plan.Enabled || plan.TotalAmount <= 0 || order.Money != plan.PriceAmount || baseMoney != plan.PriceAmount || order.PurchaseType != commerceschema.SubscriptionPurchaseTypeNormal {
				return errors.New("invalid credit pack checkout")
			}
			order.CreditPackQuota = plan.TotalAmount
			order.CreditPackPaidQuota = quotaUnitsFromUSD(order.Money)
			order.CreditPackChannelShareBPS = creditPackChannelShareBPS
			if !validCreditPackPurchaseQuota(order.CreditPackQuota, order.CreditPackPaidQuota) {
				return errors.New("credit pack bonus must be between 5% and 10%")
			}
			order.OriginalMoney = plan.PriceAmount
			if err := snapshotSubscriptionEpayCheckout(order, 0); err != nil {
				return err
			}
			return tx.Create(order).Error
		}
		baseMoney = math.Round(baseMoney*100) / 100
		if err := applyFirstPurchaseDiscountTx(tx, order, baseMoney, time.Now()); err != nil {
			return err
		}
		if order.FirstPurchaseDiscountApplied {
			if err := snapshotSubscriptionEpayCheckout(order, 0); err != nil {
				return err
			}
			return tx.Create(order).Error
		}
		prop, err := ReserveBlindBoxSubscriptionDiscountPropTx(tx, order.UserId, order.TradeNo)
		if err != nil {
			return err
		}
		if prop != nil {
			appliedRate = prop.DiscountRate
			order.Money = commercedomain.ApplyDiscountRateToMoney(baseMoney, prop.DiscountRate)
		}
		propID := 0
		if prop != nil {
			propID = prop.Id
		}
		if err := snapshotSubscriptionEpayCheckout(order, propID); err != nil {
			return err
		}
		return tx.Create(order).Error
	})
	return appliedRate, err
}

// CompleteSubscriptionOrder completes a pending subscription order idempotently.
func CompleteSubscriptionOrder(tradeNo string, providerPayload string, expectedPaymentProvider string, actualPaymentMethod string) error {
	if strings.TrimSpace(tradeNo) == "" {
		return errors.New("tradeNo is empty")
	}

	var completedOrder *commerceschema.SubscriptionOrder
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		order := &commerceschema.SubscriptionOrder{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(subscriptionTradeNoColumn()+" = ?", tradeNo).First(order).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return commerceschema.ErrSubscriptionOrderNotFound
			}
			return err
		}
		if expectedPaymentProvider != "" && order.PaymentProvider != expectedPaymentProvider {
			return commerceschema.ErrPaymentMethodMismatch
		}
		if order.PaymentProvider == commerceschema.PaymentProviderEpay {
			if actualPaymentMethod != "" && actualPaymentMethod != order.PaymentMethod {
				return commerceschema.ErrPaymentMethodMismatch
			}
			paid, err := validateEpayOrderPayment(providerPayload, order.TradeNo, order.PaymentMethod, order.Money)
			if err != nil {
				return err
			}
			if order.Status == constant.TopUpStatusSuccess {
				if extractProviderOrderID(order.ProviderPayload) != paid.TradeNo {
					return errors.New("epay original provider order mismatch")
				}
			} else {
				if order.RefundStatus != "" || order.RefundNo != "" || order.RefundProviderID != "" || order.RefundQuota != 0 || order.RefundAmount != 0 {
					return commerceschema.ErrSubscriptionOrderStatusInvalid
				}
				if order.Status != constant.TopUpStatusPending && order.Status != constant.TopUpStatusExpired {
					return commerceschema.ErrSubscriptionOrderStatusInvalid
				}
				var user identityschema.User
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id = ?", order.UserId).First(&user).Error; err != nil {
					return err
				}
				if err := consumeSubscriptionEpayOriginalPropTx(tx, order); err != nil {
					return err
				}
			}
		}
		if order.Status == constant.TopUpStatusSuccess {
			if order.FulfillmentStatus != commerceschema.SubscriptionOrderFulfillmentCompleted && order.RefundStatus == "" {
				completedCopy := *order
				completedOrder = &completedCopy
			}
			return nil
		}
		if order.Status != constant.TopUpStatusPending && order.Status != constant.TopUpStatusExpired {
			return commerceschema.ErrSubscriptionOrderStatusInvalid
		}

		order.Status = constant.TopUpStatusSuccess
		order.FulfillmentStatus = commerceschema.SubscriptionOrderFulfillmentPending
		order.CompleteTime = platformruntime.GetTimestamp()
		if providerPayload != "" {
			order.ProviderPayload = providerPayload
		}
		if actualPaymentMethod != "" && order.PaymentMethod != actualPaymentMethod {
			order.PaymentMethod = actualPaymentMethod
		}
		if err := tx.Save(order).Error; err != nil {
			return err
		}

		completedCopy := *order
		completedOrder = &completedCopy
		return nil
	})
	if err != nil {
		return err
	}

	if completedOrder != nil {
		if err := StartOrderFulfillmentWorkflow(context.Background(), completedOrder); err != nil {
			platformobservability.SysLog("start order fulfillment workflow: " + err.Error())
		}
	}
	return nil
}

// FulfillPaidSubscriptionOrder grants all purchased benefits exactly once. It is
// invoked by OrderFulfillmentWorkflow after a payment callback commits success.
func FulfillPaidSubscriptionOrder(tradeNo string) error {
	if strings.TrimSpace(tradeNo) == "" {
		return errors.New("tradeNo is empty")
	}

	var logUserID int
	var logPlanTitle string
	var logMoney float64
	var logPaymentMethod string
	var upgradeGroup string
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		order := &commerceschema.SubscriptionOrder{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(subscriptionTradeNoColumn()+" = ?", tradeNo).First(order).Error; err != nil {
			return commerceschema.ErrSubscriptionOrderNotFound
		}
		if order.Status != constant.TopUpStatusSuccess {
			return commerceschema.ErrSubscriptionOrderStatusInvalid
		}
		if order.FulfillmentStatus == commerceschema.SubscriptionOrderFulfillmentCompleted {
			return nil
		}
		if order.RefundStatus != "" || order.RefundNo != "" || order.RefundProviderID != "" || order.RefundQuota != 0 || order.RefundAmount != 0 {
			return commerceschema.ErrSubscriptionOrderStatusInvalid
		}
		if order.CreditPackQuota > 0 {
			if err := fulfillCreditPackTx(tx, order); err != nil {
				return err
			}
			logUserID, logMoney, logPaymentMethod = order.UserId, order.Money, order.PaymentMethod
			logPlanTitle = "永久额度包"
			return nil
		}
		if order.PurchaseType == "subscription_booster" {
			return errors.New("legacy subscription booster orders are no longer fulfillable")
		}
		if order.PurchaseType == commerceschema.SubscriptionPurchaseTypeFuel {
			if err := fulfillSubscriptionFuelTx(tx, order); err != nil {
				return err
			}
			order.FulfillmentStatus = commerceschema.SubscriptionOrderFulfillmentCompleted
			return tx.Save(order).Error
		}
		plan, err := getSubscriptionPlanRecordTx(tx, order.PlanId)
		if err != nil {
			return err
		}
		sub, preview, err := ApplySubscriptionPurchaseTx(tx, order.UserId, plan, "order")
		if err != nil {
			return err
		}
		if err := awardPackagePurchasePointsTx(tx, order.UserId, plan, order.Id); err != nil {
			return err
		}
		if preview != nil {
			upgradeGroup = strings.TrimSpace(sub.UpgradeGroup)
		}
		if preview != nil {
			if err := awardMonthlyPassPurchasePropTx(tx, order.UserId, plan, preview, order.Money, fmt.Sprintf("monthly-pass-order:%d", order.Id)); err != nil {
				return err
			}
		}
		if err := upsertSubscriptionTopUpTx(tx, order); err != nil {
			return err
		}
		purchaseType := commercedomain.ReferralPurchaseTypeMonthCard
		if commercedomain.IsSubscriptionDayPassPlan(plan) {
			purchaseType = commercedomain.ReferralPurchaseTypeDayPass
		}
		if err := awardReferralFirstPurchaseBonusTx(tx, order.UserId, purchaseType, "subscription_order", order.TradeNo); err != nil {
			return err
		}
		if err := ConsumeReservedBlindBoxPropByTradeNoTx(tx, tradeNo, commerceschema.BlindBoxPropOrderTypeSubscription); err != nil {
			return err
		}
		if err := ApplySubscriptionPurchaseBonusTx(tx, order.UserId, sub, plan, preview, order.TradeNo); err != nil {
			return err
		}
		if err := ApplyGroupBuyPurchaseAfterPaymentTx(tx, order, plan, sub); err != nil {
			return err
		}

		order.TargetSubscriptionId = sub.Id
		order.FulfillmentStatus = commerceschema.SubscriptionOrderFulfillmentCompleted
		if err := tx.Save(order).Error; err != nil {
			return err
		}
		logUserID = order.UserId
		logPlanTitle = plan.Title
		logMoney = order.Money
		logPaymentMethod = order.PaymentMethod
		return nil
	})
	if err != nil {
		return err
	}
	if upgradeGroup != "" && logUserID > 0 {
		_ = identitystore.UpdateUserGroupCache(logUserID, upgradeGroup)
	}
	if logUserID > 0 {
		invalidateTopUpUserCache(logUserID)
		auditapp.RecordLog(logUserID, auditschema.LogTypeTopup, fmt.Sprintf("订阅购买成功，套餐: %s，支付金额: %.2f，支付方式: %s", logPlanTitle, logMoney, logPaymentMethod))
	}
	return nil
}

// ExpireSubscriptionOrder marks a pending subscription order as expired.
func ExpireSubscriptionOrder(tradeNo string, expectedPaymentProvider string) error {
	if strings.TrimSpace(tradeNo) == "" {
		return errors.New("tradeNo is empty")
	}

	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		order := &commerceschema.SubscriptionOrder{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(subscriptionTradeNoColumn()+" = ?", tradeNo).First(order).Error; err != nil {
			return commerceschema.ErrSubscriptionOrderNotFound
		}
		if expectedPaymentProvider != "" && order.PaymentProvider != expectedPaymentProvider {
			return commerceschema.ErrPaymentMethodMismatch
		}
		if order.Status != constant.TopUpStatusPending {
			return nil
		}

		order.Status = constant.TopUpStatusExpired
		order.CompleteTime = platformruntime.GetTimestamp()
		if err := tx.Save(order).Error; err != nil {
			return err
		}
		return ReleaseReservedBlindBoxPropByTradeNoTx(tx, tradeNo, commerceschema.BlindBoxPropOrderTypeSubscription)
	})
}

// CancelPendingSubscriptionOrder expires a user's unpaid subscription order.
func CancelPendingSubscriptionOrder(userID int, tradeNo string) error {
	if userID <= 0 || strings.TrimSpace(tradeNo) == "" {
		return errors.New("invalid subscription order cancellation")
	}
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		order := &commerceschema.SubscriptionOrder{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(subscriptionTradeNoColumn()+" = ? AND user_id = ?", tradeNo, userID).First(order).Error; err != nil {
			return commerceschema.ErrSubscriptionOrderNotFound
		}
		if order.Status != constant.TopUpStatusPending {
			return nil
		}
		order.Status = constant.TopUpStatusExpired
		order.CompleteTime = platformruntime.GetTimestamp()
		if err := tx.Save(order).Error; err != nil {
			return err
		}
		return ReleaseReservedBlindBoxPropByTradeNoTx(tx, tradeNo, commerceschema.BlindBoxPropOrderTypeSubscription)
	})
}

// ExpireDueSubscriptionOrders releases stale unpaid checkouts. Late Epay
// payments require original checkout evidence; released discount reservations
// fail closed for manual review.
func ExpireDueSubscriptionOrders(limit int) (int, error) {
	if limit <= 0 {
		limit = 300
	}
	now := platformruntime.GetTimestamp()
	cutoff := now - int64(PendingSubscriptionOrderExpiry().Seconds())
	var ids []int
	if err := platformdb.DB.Model(&commerceschema.SubscriptionOrder{}).
		Where("status = ? AND create_time > 0 AND create_time <= ?", constant.TopUpStatusPending, cutoff).
		Order("create_time asc, id asc").Limit(limit).Pluck("id", &ids).Error; err != nil {
		return 0, err
	}

	expired := 0
	for _, id := range ids {
		err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
			var order commerceschema.SubscriptionOrder
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id = ? AND status = ? AND create_time <= ?", id, constant.TopUpStatusPending, cutoff).
				First(&order).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil
				}
				return err
			}
			order.Status = constant.TopUpStatusExpired
			order.CompleteTime = now
			if err := tx.Save(&order).Error; err != nil {
				return err
			}
			if err := ReleaseReservedBlindBoxPropByTradeNoTx(tx, order.TradeNo, commerceschema.BlindBoxPropOrderTypeSubscription); err != nil {
				return err
			}
			expired++
			return nil
		})
		if err != nil {
			return expired, err
		}
	}
	return expired, nil
}

func upsertSubscriptionTopUpTx(tx *gorm.DB, order *commerceschema.SubscriptionOrder) error {
	if tx == nil || order == nil {
		return errors.New("invalid subscription order")
	}

	now := platformruntime.GetTimestamp()
	topup := &commerceschema.TopUp{}
	if err := tx.Where("trade_no = ?", order.TradeNo).First(topup).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			topup = &commerceschema.TopUp{
				UserId:          order.UserId,
				Amount:          0,
				Money:           order.Money,
				TradeNo:         order.TradeNo,
				PaymentMethod:   order.PaymentMethod,
				PaymentProvider: order.PaymentProvider,
				CreateTime:      order.CreateTime,
				CompleteTime:    now,
				Status:          constant.TopUpStatusSuccess,
			}
			return tx.Create(topup).Error
		}
		return err
	}

	topup.Money = order.Money
	if order.PaymentProvider != "" {
		topup.PaymentProvider = order.PaymentProvider
	}
	if topup.PaymentMethod == "" {
		topup.PaymentMethod = order.PaymentMethod
	} else if topup.PaymentMethod != order.PaymentMethod {
		return commerceschema.ErrPaymentMethodMismatch
	}
	if topup.CreateTime == 0 {
		topup.CreateTime = order.CreateTime
	}
	topup.CompleteTime = now
	topup.Status = constant.TopUpStatusSuccess
	return tx.Save(topup).Error
}

func subscriptionTradeNoColumn() string {
	if platformdb.UsingPostgreSQL {
		return `"trade_no"`
	}
	return "`trade_no`"
}
