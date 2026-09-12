package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/constant"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

const refundFeeRate = 0.02

func ListRefundableOrders(userID int) ([]commerceschema.RefundableOrder, error) {
	if userID <= 0 {
		return nil, errors.New("invalid user")
	}
	var topups []commerceschema.TopUp
	if err := platformdb.DB.Where("user_id = ? AND status = ? AND payment_provider = ?", userID, constant.TopUpStatusSuccess, commerceschema.PaymentProviderEpay).Order("id desc").Limit(100).Find(&topups).Error; err != nil {
		return nil, err
	}
	var orders []commerceschema.SubscriptionOrder
	if err := platformdb.DB.Where("user_id = ? AND status = ? AND payment_provider = ? AND purchase_type <> ?", userID, constant.TopUpStatusSuccess, commerceschema.PaymentProviderEpay, commerceschema.SubscriptionPurchaseTypeFuel).Order("id desc").Limit(100).Find(&orders).Error; err != nil {
		return nil, err
	}
	result := make([]commerceschema.RefundableOrder, 0, len(topups)+len(orders))
	for index := range topups {
		item := refundableTopup(userID, &topups[index])
		result = append(result, item)
	}
	for index := range orders {
		item := refundableSubscription(userID, &orders[index])
		result = append(result, item)
	}
	return result, nil
}

func refundableTopup(userID int, order *commerceschema.TopUp) commerceschema.RefundableOrder {
	item := commerceschema.RefundableOrder{OrderType: commerceschema.RefundOrderTypeBalance, TradeNo: order.TradeNo, PaymentMethod: order.PaymentMethod, CreatedAt: order.CreateTime, PaidAmount: order.Money, RefundStatus: order.RefundStatus}
	if order.UserId != userID || order.RefundStatus == commerceschema.RefundStatusSuccess {
		return item
	}
	var remaining int64
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		remaining, err = billingapp.TopupFundingLotRemaining(tx, userID, order.TradeNo)
		return err
	})
	if err != nil {
		item.UnavailableReason = "该订单没有可识别的充值余额"
		return item
	}
	item.TotalQuota = order.Amount
	item.RemainingQuota = remaining
	item.UsedQuota = order.Amount - remaining
	item.GrossRefund, item.FeeAmount, item.RefundAmount = refundMoney(order.Money, remaining, order.Amount)
	item.Refundable = (order.RefundStatus == "" || order.RefundStatus == commerceschema.RefundStatusFailed) && remaining > 0 && item.RefundAmount >= 0.01 && strings.TrimSpace(extractProviderOrderID(order.ProviderPayload)) != ""
	if !item.Refundable && item.UnavailableReason == "" {
		item.UnavailableReason = refundStatusReason(order.RefundStatus)
		if item.UnavailableReason == "" {
			item.UnavailableReason = refundUnavailableReason(order.ProviderPayload, remaining, item.RefundAmount)
		}
	}
	return item
}

func refundableSubscription(userID int, order *commerceschema.SubscriptionOrder) commerceschema.RefundableOrder {
	item := commerceschema.RefundableOrder{OrderType: commerceschema.RefundOrderTypeSubscription, TradeNo: order.TradeNo, PaymentMethod: order.PaymentMethod, CreatedAt: order.CreateTime, PaidAmount: order.Money, RefundStatus: order.RefundStatus}
	if order.UserId != userID || order.TargetSubscriptionId <= 0 || order.RefundStatus == commerceschema.RefundStatusSuccess {
		return item
	}
	var sub commerceschema.UserSubscription
	if err := platformdb.DB.Where("id = ? AND user_id = ?", order.TargetSubscriptionId, userID).First(&sub).Error; err != nil {
		item.UnavailableReason = "找不到对应套餐使用记录"
		return item
	}
	item.TotalQuota = sub.AmountTotal
	item.UsedQuota = sub.AmountUsed
	item.RemainingQuota = sub.AmountTotal - sub.AmountUsed
	if item.RemainingQuota < 0 {
		item.RemainingQuota = 0
	}
	item.GrossRefund, item.FeeAmount, item.RefundAmount = refundMoney(order.Money, item.RemainingQuota, item.TotalQuota)
	item.Refundable = (order.RefundStatus == "" || order.RefundStatus == commerceschema.RefundStatusFailed) && item.RemainingQuota > 0 && item.RefundAmount >= 0.01 && strings.TrimSpace(extractProviderOrderID(order.ProviderPayload)) != ""
	if !item.Refundable {
		item.UnavailableReason = refundStatusReason(order.RefundStatus)
		if item.UnavailableReason == "" {
			item.UnavailableReason = refundUnavailableReason(order.ProviderPayload, item.RemainingQuota, item.RefundAmount)
		}
	}
	return item
}

func refundMoney(paid float64, remaining, total int64) (float64, float64, float64) {
	if paid <= 0 || remaining <= 0 || total <= 0 {
		return 0, 0, 0
	}
	gross := math.Round(paid*float64(remaining)/float64(total)*100) / 100
	fee := math.Round(gross*refundFeeRate*100) / 100
	net := math.Round((gross-fee)*100) / 100
	return gross, fee, net
}

func refundUnavailableReason(providerPayload string, remaining int64, refundAmount float64) string {
	if remaining <= 0 {
		return "该订单额度已使用完，不支持退款"
	}
	if refundAmount < 0.01 {
		return "可退款金额低于 0.01 元"
	}
	if strings.TrimSpace(extractProviderOrderID(providerPayload)) == "" {
		return "缺少 JianPay 平台订单号"
	}
	return "当前订单不可退款"
}

func refundStatusReason(status string) string {
	switch status {
	case commerceschema.RefundStatusProcessing:
		return "退款处理中"
	case commerceschema.RefundStatusSuccess:
		return "退款已完成"
	case commerceschema.RefundStatusFailed:
		return "上次退款失败，可联系管理员处理"
	default:
		return ""
	}
}

func CreateUserRefund(userID int, req commerceschema.RefundRequest) (*commerceschema.RefundResult, error) {
	if userID <= 0 || strings.TrimSpace(req.TradeNo) == "" {
		return nil, errors.New("退款订单参数无效")
	}
	refundNo := fmt.Sprintf("RFUSR%d%s%d", userID, platformruntime.GetRandomString(8), time.Now().Unix())
	var orderType string
	var providerOrderID string
	var amountCents int64
	var refundAmount float64
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		switch req.OrderType {
		case commerceschema.RefundOrderTypeBalance:
			var order commerceschema.TopUp
			if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("user_id = ? AND trade_no = ?", userID, req.TradeNo).First(&order).Error; err != nil {
				return errors.New("充值订单不存在")
			}
			item := refundableTopupTx(tx, userID, &order)
			if !item.Refundable {
				return errors.New(item.UnavailableReason)
			}
			order.RefundStatus = commerceschema.RefundStatusProcessing
			order.RefundNo = refundNo
			order.RefundAmount = item.RefundAmount
			order.RefundQuota = item.RemainingQuota
			order.RefundUpdatedAt = platformruntime.GetTimestamp()
			if err := tx.Save(&order).Error; err != nil {
				return err
			}
			orderType, providerOrderID, amountCents, refundAmount = req.OrderType, extractProviderOrderID(order.ProviderPayload), int64(math.Round(item.RefundAmount*100)), item.RefundAmount
		case commerceschema.RefundOrderTypeSubscription:
			var order commerceschema.SubscriptionOrder
			if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("user_id = ? AND trade_no = ?", userID, req.TradeNo).First(&order).Error; err != nil {
				return errors.New("套餐订单不存在")
			}
			item := refundableSubscriptionTx(tx, userID, &order)
			if !item.Refundable {
				return errors.New(item.UnavailableReason)
			}
			order.RefundStatus = commerceschema.RefundStatusProcessing
			order.RefundNo = refundNo
			order.RefundAmount = item.RefundAmount
			order.RefundQuota = item.RemainingQuota
			order.RefundUpdatedAt = platformruntime.GetTimestamp()
			if err := tx.Save(&order).Error; err != nil {
				return err
			}
			orderType, providerOrderID, amountCents, refundAmount = req.OrderType, extractProviderOrderID(order.ProviderPayload), int64(math.Round(item.RefundAmount*100)), item.RefundAmount
		default:
			return errors.New("不支持的退款订单类型")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	response, err := JianPayRefundCreate(providerOrderID, refundNo, amountCents, "用户申请未使用额度退款（扣除2%手续费）")
	if err != nil {
		_ = setRefundStatus(userID, orderType, req.TradeNo, commerceschema.RefundStatusFailed, "", err.Error())
		return nil, err
	}
	if err := saveRefundProviderState(userID, orderType, req.TradeNo, response); err != nil {
		return nil, err
	}
	if response.Status == 2 {
		if err := finalizeUserRefund(userID, orderType, req.TradeNo, refundNo, refundAmount); err != nil {
			return nil, err
		}
	}
	return &commerceschema.RefundResult{OrderType: orderType, TradeNo: req.TradeNo, RefundNo: refundNo, RefundID: response.RefundID, RefundAmount: refundAmount, Status: refundStatusFromJianPay(response.Status)}, nil
}

func SyncUserRefund(userID int, refundNo string) (*commerceschema.RefundResult, error) {
	var topup commerceschema.TopUp
	if err := platformdb.DB.Where("user_id = ? AND refund_no = ?", userID, refundNo).First(&topup).Error; err == nil {
		return syncRefundOrder(userID, commerceschema.RefundOrderTypeBalance, topup.TradeNo, refundNo, topup.RefundProviderID, topup.RefundAmount)
	}
	var order commerceschema.SubscriptionOrder
	if err := platformdb.DB.Where("user_id = ? AND refund_no = ?", userID, refundNo).First(&order).Error; err != nil {
		return nil, errors.New("退款订单不存在")
	}
	return syncRefundOrder(userID, commerceschema.RefundOrderTypeSubscription, order.TradeNo, refundNo, order.RefundProviderID, order.RefundAmount)
}

func syncRefundOrder(userID int, orderType, tradeNo, refundNo, providerID string, amount float64) (*commerceschema.RefundResult, error) {
	response, err := JianPayRefundQuery(providerID, refundNo)
	if err != nil {
		return nil, err
	}
	if err := saveRefundProviderState(userID, orderType, tradeNo, response); err != nil {
		return nil, err
	}
	if response.Status == 2 {
		if err := finalizeUserRefund(userID, orderType, tradeNo, refundNo, amount); err != nil {
			return nil, err
		}
	}
	return &commerceschema.RefundResult{OrderType: orderType, TradeNo: tradeNo, RefundNo: refundNo, RefundID: response.RefundID, RefundAmount: amount, Status: refundStatusFromJianPay(response.Status)}, nil
}

func finalizeUserRefund(userID int, orderType, tradeNo, refundNo string, refundAmount float64) error {
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		now := platformruntime.GetTimestamp()
		if orderType == commerceschema.RefundOrderTypeBalance {
			var order commerceschema.TopUp
			if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("user_id = ? AND trade_no = ?", userID, tradeNo).First(&order).Error; err != nil {
				return err
			}
			remaining := order.RefundQuota
			if remaining <= 0 {
				return errors.New("充值余额已处理或不可退款")
			}
			if err := billingapp.RefundTopupFundingLotTx(tx, userID, tradeNo, remaining, "refund:"+refundNo); err != nil {
				return err
			}
			order.RefundStatus, order.RefundNo, order.RefundAmount, order.RefundUpdatedAt = commerceschema.RefundStatusSuccess, refundNo, refundAmount, now
			return tx.Save(&order).Error
		}
		var order commerceschema.SubscriptionOrder
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("user_id = ? AND trade_no = ?", userID, tradeNo).First(&order).Error; err != nil {
			return err
		}
		var sub commerceschema.UserSubscription
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("id = ? AND user_id = ?", order.TargetSubscriptionId, userID).First(&sub).Error; err != nil {
			return err
		}
		if order.RefundQuota <= 0 || order.RefundQuota > sub.AmountTotal-sub.AmountUsed {
			return errors.New("套餐可退款额度已变化")
		}
		sub.AmountTotal -= order.RefundQuota
		sub.PeriodAmount -= minInt64(order.RefundQuota, sub.PeriodAmount-sub.PeriodUsed)
		sub.Status = commerceschema.SubscriptionStatusSettledCancelled
		if err := tx.Save(&sub).Error; err != nil {
			return err
		}
		order.RefundStatus, order.RefundNo, order.RefundAmount, order.RefundUpdatedAt = commerceschema.RefundStatusSuccess, refundNo, refundAmount, now
		return tx.Save(&order).Error
	})
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}

func refundableTopupTx(tx *gorm.DB, userID int, order *commerceschema.TopUp) commerceschema.RefundableOrder {
	item := commerceschema.RefundableOrder{OrderType: commerceschema.RefundOrderTypeBalance, TradeNo: order.TradeNo, PaymentMethod: order.PaymentMethod, CreatedAt: order.CreateTime, PaidAmount: order.Money, RefundStatus: order.RefundStatus}
	remaining, err := billingapp.TopupFundingLotRemaining(tx, userID, order.TradeNo)
	if err != nil {
		item.UnavailableReason = "该订单没有可识别的充值余额"
		return item
	}
	item.TotalQuota, item.RemainingQuota, item.UsedQuota = order.Amount, remaining, order.Amount-remaining
	item.GrossRefund, item.FeeAmount, item.RefundAmount = refundMoney(order.Money, remaining, order.Amount)
	item.Refundable = (order.RefundStatus == "" || order.RefundStatus == commerceschema.RefundStatusFailed) && remaining > 0 && item.RefundAmount >= 0.01 && strings.TrimSpace(extractProviderOrderID(order.ProviderPayload)) != ""
	if !item.Refundable {
		item.UnavailableReason = refundUnavailableReason(order.ProviderPayload, remaining, item.RefundAmount)
	}
	return item
}

func refundableSubscriptionTx(tx *gorm.DB, userID int, order *commerceschema.SubscriptionOrder) commerceschema.RefundableOrder {
	item := refundableSubscription(userID, order)
	var sub commerceschema.UserSubscription
	if err := tx.Where("id = ? AND user_id = ?", order.TargetSubscriptionId, userID).First(&sub).Error; err != nil {
		item.Refundable = false
		item.UnavailableReason = "找不到对应套餐使用记录"
		return item
	}
	item.TotalQuota, item.UsedQuota, item.RemainingQuota = sub.AmountTotal, sub.AmountUsed, sub.AmountTotal-sub.AmountUsed
	if item.RemainingQuota < 0 {
		item.RemainingQuota = 0
	}
	item.GrossRefund, item.FeeAmount, item.RefundAmount = refundMoney(order.Money, item.RemainingQuota, item.TotalQuota)
	item.Refundable = (order.RefundStatus == "" || order.RefundStatus == commerceschema.RefundStatusFailed) && item.RemainingQuota > 0 && item.RefundAmount >= 0.01 && strings.TrimSpace(extractProviderOrderID(order.ProviderPayload)) != ""
	if !item.Refundable {
		item.UnavailableReason = refundStatusReason(order.RefundStatus)
		if item.UnavailableReason == "" {
			item.UnavailableReason = refundUnavailableReason(order.ProviderPayload, item.RemainingQuota, item.RefundAmount)
		}
	}
	return item
}

func saveRefundProviderState(userID int, orderType, tradeNo string, response *JianPayRefundResult) error {
	status := refundStatusFromJianPay(response.Status)
	if response.Status == 2 {
		status = commerceschema.RefundStatusProcessing
	}
	return setRefundStatus(userID, orderType, tradeNo, status, response.RefundID, response.ErrorMessage)
}

func setRefundStatus(userID int, orderType, tradeNo, status, providerID, _ string) error {
	updates := map[string]any{"refund_status": status, "refund_provider_id": providerID, "refund_updated_at": platformruntime.GetTimestamp()}
	if orderType == commerceschema.RefundOrderTypeBalance {
		return platformdb.DB.Model(&commerceschema.TopUp{}).Where("user_id = ? AND trade_no = ?", userID, tradeNo).Updates(updates).Error
	}
	return platformdb.DB.Model(&commerceschema.SubscriptionOrder{}).Where("user_id = ? AND trade_no = ?", userID, tradeNo).Updates(updates).Error
}

func refundStatusFromJianPay(status int) string {
	switch status {
	case 2:
		return commerceschema.RefundStatusSuccess
	case 3:
		return commerceschema.RefundStatusFailed
	default:
		return commerceschema.RefundStatusProcessing
	}
}

func extractProviderOrderID(payload string) string {
	var data map[string]any
	if json.Unmarshal([]byte(payload), &data) != nil {
		return ""
	}
	for _, key := range []string{"trade_no", "TradeNo", "orderId", "order_id"} {
		if value, ok := data[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
