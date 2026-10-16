package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/sh2001sh/new-api/constant"
	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"math"
	"strconv"
	"strings"
)

type epayTopupCheckoutSnapshot struct {
	Version int `json:"version"`
	PropID  int `json:"prop_id"`
}

// Keep the original card identity even after expiry releases its reservation.
// This is local checkout evidence, never evidence that a provider was paid.
func snapshotEpayTopupCheckout(topUp *commerceschema.TopUp, propID int) error {
	payload := map[string]any{}
	if topUp.ProviderPayload != "" {
		if err := json.Unmarshal([]byte(topUp.ProviderPayload), &payload); err != nil {
			return err
		}
		if payload == nil {
			return errors.New("invalid epay checkout payload")
		}
	}
	payload["epay_checkout"] = epayTopupCheckoutSnapshot{Version: 1, PropID: propID}
	data, err := json.Marshal(payload)
	if err == nil {
		topUp.ProviderPayload = string(data)
	}
	return err
}

// Only HandleEpayWebhook calls this after SDK signature and merchant validation.
// Other providers and administrator completion retain their pending-only rule.
func completeVerifiedEpayTopUp(verified *epay.VerifyRes, params map[string]string) (*commerceschema.TopUp, int, error) {
	if verified == nil || !verified.VerifyStatus || verified.TradeStatus != epay.StatusTradeSuccess ||
		strings.TrimSpace(verified.ServiceTradeNo) == "" || strings.TrimSpace(verified.TradeNo) == "" {
		return nil, 0, errors.New("invalid verified epay payment")
	}
	// Epay checkout uses the CNY submit.php protocol and sends no currency field.
	// An explicit conflicting currency cannot change that original denomination.
	if currency := params["currency"]; currency != "" && currency != "CNY" {
		return nil, 0, errors.New("epay currency mismatch")
	}
	paidMoney, err := decimal.NewFromString(verified.Money)
	if err != nil || !paidMoney.IsPositive() || !paidMoney.Equal(paidMoney.Round(2)) {
		return nil, 0, errors.New("invalid epay payment amount")
	}
	var completed *commerceschema.TopUp
	creditedQuota := 0
	err = platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var order commerceschema.TopUp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("trade_no = ?", verified.ServiceTradeNo).First(&order).Error; err != nil {
			return err
		}
		if order.PaymentProvider != commerceschema.PaymentProviderEpay || order.PaymentMethod == "" || order.PaymentMethod != verified.Type {
			return commerceschema.ErrPaymentMethodMismatch
		}
		if math.IsNaN(order.Money) || math.IsInf(order.Money, 0) || order.Money <= 0 {
			return errors.New("invalid epay order amount")
		}
		expectedMoney, err := decimal.NewFromString(strconv.FormatFloat(order.Money, 'f', 2, 64))
		if err != nil || !paidMoney.Equal(expectedMoney) {
			return errors.New("epay payment amount mismatch")
		}
		if order.ExternalPaymentID != "" && order.ExternalPaymentID != verified.TradeNo {
			return errors.New("epay provider order mismatch")
		}
		if order.Status == constant.TopUpStatusSuccess {
			if order.ExternalPaymentID == "" {
				var previous map[string]string
				if json.Unmarshal([]byte(order.ProviderPayload), &previous) != nil || previous["trade_no"] != verified.TradeNo {
					return errors.New("epay original provider order evidence missing or conflicting")
				}
			}
			// A replay acknowledges the original payment without touching a refund,
			// its funding lots, or the immutable payment evidence.
			completed = &order
			return nil
		}
		if (order.Status != constant.TopUpStatusPending && order.Status != constant.TopUpStatusExpired) ||
			order.RefundStatus != "" || order.RefundNo != "" || order.RefundProviderID != "" || order.RefundAmount != 0 || order.RefundQuota != 0 {
			return commerceschema.ErrTopUpStatusInvalid
		}
		quota := calculateTopUpQuotaToAdd(&order)
		if quota <= 0 {
			return errors.New("invalid topup quota")
		}
		if err := consumeEpayTopupOriginalPropTx(tx, &order); err != nil {
			return err
		}
		payload, err := json.Marshal(params)
		if err != nil {
			return err
		}
		previousStatus := order.Status
		order.Status = constant.TopUpStatusSuccess
		order.CompleteTime = platformruntime.GetTimestamp()
		order.ExternalPaymentID = verified.TradeNo
		order.ProviderPayload = string(payload)
		result := tx.Model(&commerceschema.TopUp{}).Where("id = ? AND status = ?", order.Id, previousStatus).
			Updates(map[string]any{"status": order.Status, "complete_time": order.CompleteTime, "external_payment_id": order.ExternalPaymentID, "provider_payload": order.ProviderPayload})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return commerceschema.ErrTopUpStatusInvalid
		}
		if err := creditTopUpQuotaTx(tx, &order, quota, "", ""); err != nil {
			return err
		}
		completed, creditedQuota = &order, quota
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	if creditedQuota > 0 {
		invalidateTopUpUserCache(completed.UserId)
	}
	return completed, creditedQuota, nil
}

func consumeEpayTopupOriginalPropTx(tx *gorm.DB, order *commerceschema.TopUp) error {
	var envelope struct {
		Checkout *epayTopupCheckoutSnapshot `json:"epay_checkout"`
	}
	if order.ProviderPayload != "" {
		if err := json.Unmarshal([]byte(order.ProviderPayload), &envelope); err != nil {
			return errors.New("invalid epay checkout evidence")
		}
	}
	var props []commerceschema.BlindBoxProp
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("reserved_order_trade_no = ? AND reserved_order_type = ? AND status = ?", order.TradeNo, commerceschema.BlindBoxPropOrderTypeTopup, commerceschema.BlindBoxPropStatusReserved).Limit(2).Find(&props).Error; err != nil {
		return err
	}
	if len(props) > 1 {
		return errors.New("epay discount reservation conflict")
	}
	if envelope.Checkout != nil {
		if envelope.Checkout.Version != 1 || envelope.Checkout.PropID < 0 ||
			(envelope.Checkout.PropID == 0 && len(props) != 0) ||
			(envelope.Checkout.PropID > 0 && (len(props) != 1 || props[0].Id != envelope.Checkout.PropID)) {
			return errors.New("epay original discount prop released or conflicting; manual review required")
		}
	} else if len(props) == 0 {
		// Legacy expiry or a conflicting reservation can erase the original
		// card identity even while an order is pending. PaidQuota alone cannot
		// prove no card was used. Never reconstruct it from mutable settings.
		return errors.New("epay original discount evidence missing; manual review required")
	}
	if len(props) == 0 {
		return nil
	}
	prop := props[0]
	if prop.UserId != order.UserId || prop.PropType != commerceschema.BlindBoxPropTypeTopupDiscount90 {
		return errors.New("epay original discount prop ownership mismatch")
	}
	now := platformruntime.GetTimestamp()
	result := tx.Model(&commerceschema.BlindBoxProp{}).
		Where("id = ? AND user_id = ? AND status = ? AND reserved_order_trade_no = ? AND reserved_order_type = ?", prop.Id, order.UserId, commerceschema.BlindBoxPropStatusReserved, order.TradeNo, commerceschema.BlindBoxPropOrderTypeTopup).
		Updates(map[string]any{"status": commerceschema.BlindBoxPropStatusUsed, "used_at": now, "reserved_order_type": "", "reserved_order_trade_no": "", "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("epay original discount prop changed")
	}
	return nil
}

func calculateTopUpQuotaToAdd(topUp *commerceschema.TopUp) int {
	if topUp == nil {
		return 0
	}
	dQuotaPerUnit := decimal.NewFromFloat(platformruntime.QuotaPerUnit)
	switch {
	case topUp.NormalizedWalletType() == commerceschema.WalletTypeClaude:
		return int(decimal.NewFromInt(topUp.Amount).Mul(dQuotaPerUnit).IntPart())
	case topUp.PaymentProvider == commerceschema.PaymentProviderCreem:
		return int(topUp.Amount)
	default:
		return int(decimal.NewFromInt(topUp.Amount).Mul(dQuotaPerUnit).IntPart())
	}
}

func creditTopUpQuotaTx(tx *gorm.DB, topUp *commerceschema.TopUp, quotaToAdd int, customerID string, customerEmail string) error {
	if topUp == nil || topUp.UserId <= 0 {
		return errors.New("invalid topup")
	}
	if quotaToAdd <= 0 {
		return errors.New("invalid topup quota")
	}

	updateFields := map[string]interface{}{}
	if err := billingapp.CreditClaudeWalletQuotaTx(tx, topUp.UserId, quotaToAdd, fmt.Sprintf("topup:%s:unified", topUp.TradeNo), "topup_credit"); err != nil {
		return err
	}
	if topUp.PaidQuota > 0 {
		if err := billingapp.SnapshotTopupPaidQuotaTx(tx, topUp.TradeNo, topUp.PaidQuota); err != nil {
			return err
		}
	}
	if topUp.PaymentProvider == commerceschema.PaymentProviderStripe && customerID != "" {
		updateFields["stripe_customer"] = customerID
	}
	if topUp.PaymentProvider == commerceschema.PaymentProviderCreem && customerEmail != "" {
		var user identityschema.User
		if err := tx.Where("id = ?", topUp.UserId).First(&user).Error; err != nil {
			return err
		}
		if user.Email == "" {
			updateFields["email"] = customerEmail
		}
	}
	if len(updateFields) == 0 {
		return nil
	}
	return tx.Model(&identityschema.User{}).Where("id = ?", topUp.UserId).Updates(updateFields).Error
}

func invalidateTopUpUserCache(userID int) {
	if userID <= 0 {
		return
	}
	_ = identitystore.InvalidateUserCache(userID)
}

func topUpWalletLogLabel(topUp *commerceschema.TopUp) string {
	return "通用额度"
}

// CompleteTopUpByTradeNo completes a pending top-up order and credits quota to the user.
func CompleteTopUpByTradeNo(tradeNo string, expectedPaymentProvider string, actualPaymentMethod string, customerID string, customerEmail string) (*commerceschema.TopUp, int, error) {
	if tradeNo == "" {
		return nil, 0, errors.New("missing trade no")
	}

	var completedTopUp *commerceschema.TopUp
	var quotaToAdd int
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		topUp := &commerceschema.TopUp{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(topUpTradeNoColumn()+" = ?", tradeNo).First(topUp).Error; err != nil {
			return commerceschema.ErrTopUpNotFound
		}
		if expectedPaymentProvider != "" && topUp.PaymentProvider != expectedPaymentProvider {
			return commerceschema.ErrPaymentMethodMismatch
		}
		if topUp.Status == constant.TopUpStatusSuccess {
			completedTopUp = topUp
			return nil
		}
		if topUp.Status != constant.TopUpStatusPending {
			return commerceschema.ErrTopUpStatusInvalid
		}
		if actualPaymentMethod != "" {
			topUp.PaymentMethod = actualPaymentMethod
		}

		quotaToAdd = calculateTopUpQuotaToAdd(topUp)
		if quotaToAdd <= 0 {
			return errors.New("invalid topup quota")
		}

		topUp.CompleteTime = platformruntime.GetTimestamp()
		topUp.Status = constant.TopUpStatusSuccess
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}
		if err := creditTopUpQuotaTx(tx, topUp, quotaToAdd, customerID, customerEmail); err != nil {
			return err
		}
		if err := ConsumeReservedBlindBoxPropByTradeNoTx(tx, tradeNo, commerceschema.BlindBoxPropOrderTypeTopup); err != nil {
			return err
		}
		completedTopUp = topUp
		return nil
	})
	if err == nil && completedTopUp != nil {
		invalidateTopUpUserCache(completedTopUp.UserId)
	}
	return completedTopUp, quotaToAdd, err
}

// ManualCompleteTopUp force-completes a pending top-up order for an administrator.
func ManualCompleteTopUp(tradeNo string, callerIP string) error {
	if tradeNo == "" {
		return errors.New("未提供订单号")
	}

	var userID int
	var quotaToAdd int
	var payMoney float64
	var paymentMethod string
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		topUp := &commerceschema.TopUp{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(topUpTradeNoColumn()+" = ?", tradeNo).First(topUp).Error; err != nil {
			return errors.New("充值订单不存在")
		}
		if topUp.Status == constant.TopUpStatusSuccess {
			userID = topUp.UserId
			payMoney = topUp.Money
			paymentMethod = topUp.PaymentMethod
			return nil
		}
		if topUp.Status != constant.TopUpStatusPending {
			return errors.New("订单状态不是待支付，无法补单")
		}

		quotaToAdd = calculateTopUpQuotaToAdd(topUp)
		if quotaToAdd <= 0 {
			return errors.New("无效的充值额度")
		}

		topUp.CompleteTime = platformruntime.GetTimestamp()
		topUp.Status = constant.TopUpStatusSuccess
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}
		if err := creditTopUpQuotaTx(tx, topUp, quotaToAdd, "", ""); err != nil {
			return err
		}
		if err := ConsumeReservedBlindBoxPropByTradeNoTx(tx, tradeNo, commerceschema.BlindBoxPropOrderTypeTopup); err != nil {
			return err
		}

		userID = topUp.UserId
		payMoney = topUp.Money
		paymentMethod = topUp.PaymentMethod
		return nil
	})
	if err != nil {
		return err
	}

	invalidateTopUpUserCache(userID)
	auditapp.RecordTopupLog(userID, fmt.Sprintf("管理员补单成功，充值金额: %v，支付金额：%f", logger.FormatQuota(quotaToAdd), payMoney), callerIP, paymentMethod, "admin")
	return nil
}

// Recharge completes a Stripe top-up order after payment succeeds.
func Recharge(referenceID string, customerID string, callerIP string) error {
	if referenceID == "" {
		return errors.New("未提供支付单号")
	}

	topUp, quotaToAdd, err := CompleteTopUpByTradeNo(referenceID, commerceschema.PaymentProviderStripe, "", customerID, "")
	if err != nil {
		platformobservability.SysError("topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}

	auditapp.RecordTopupLog(topUp.UserId, fmt.Sprintf("Stripe topup success, wallet: %s, quota: %v, paid: %.2f", topUpWalletLogLabel(topUp), logger.FormatQuota(quotaToAdd), topUp.Money), callerIP, topUp.PaymentMethod, commerceschema.PaymentMethodStripe)
	return nil
}

// RechargeCreem completes a Creem top-up order after payment succeeds.
func RechargeCreem(referenceID string, customerEmail string, _ string, callerIP string) error {
	if referenceID == "" {
		return errors.New("未提供支付单号")
	}

	topUp, quotaToAdd, err := CompleteTopUpByTradeNo(referenceID, commerceschema.PaymentProviderCreem, "", "", customerEmail)
	if err != nil {
		platformobservability.SysError("creem topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}

	auditapp.RecordTopupLog(topUp.UserId, fmt.Sprintf("使用Creem充值成功，充值额度: %v，支付金额：%.2f", quotaToAdd, topUp.Money), callerIP, topUp.PaymentMethod, commerceschema.PaymentMethodCreem)
	return nil
}

// RechargeWaffo completes a Waffo top-up order after payment succeeds.
func RechargeWaffo(tradeNo string, callerIP string) error {
	if tradeNo == "" {
		return errors.New("未提供支付单号")
	}

	topUp, quotaToAdd, err := CompleteTopUpByTradeNo(tradeNo, commerceschema.PaymentProviderWaffo, "", "", "")
	if err != nil {
		platformobservability.SysError("waffo topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}
	if quotaToAdd > 0 {
		auditapp.RecordTopupLog(topUp.UserId, fmt.Sprintf("Waffo充值成功，充值额度: %v，支付金额: %.2f", logger.FormatQuota(quotaToAdd), topUp.Money), callerIP, topUp.PaymentMethod, commerceschema.PaymentMethodWaffo)
	}
	return nil
}

// RechargeWaffoPancake completes a Waffo Pancake top-up order after payment succeeds.
func RechargeWaffoPancake(tradeNo string) error {
	if tradeNo == "" {
		return errors.New("未提供支付单号")
	}

	topUp, quotaToAdd, err := CompleteTopUpByTradeNo(tradeNo, commerceschema.PaymentProviderWaffoPancake, "", "", "")
	if err != nil {
		platformobservability.SysError("waffo pancake topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}
	if quotaToAdd > 0 {
		auditapp.RecordLog(topUp.UserId, auditschema.LogTypeTopup, fmt.Sprintf("Waffo Pancake充值成功，充值额度: %v，支付金额: %.2f", logger.FormatQuota(quotaToAdd), topUp.Money))
	}
	return nil
}
