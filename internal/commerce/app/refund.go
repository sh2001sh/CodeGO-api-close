package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/constant"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const refundFeeRate = 0.02

var errRefundHistoricalReview = errors.New("历史退款缺少可核验的原生冻结记录，请按原退款单号核验支付方后人工处理")

func listRefundableOrders(userID int) ([]commerceschema.RefundableOrder, error) {
	if userID <= 0 {
		return nil, errors.New("invalid user")
	}
	var topups []commerceschema.TopUp
	if err := platformdb.DB.Where("user_id = ? AND status = ? AND payment_provider IN ?", userID, constant.TopUpStatusSuccess, []string{commerceschema.PaymentProviderEpay, commerceschema.PaymentProviderStripe}).Order("id desc").Limit(100).Find(&topups).Error; err != nil {
		return nil, err
	}
	var orders []commerceschema.SubscriptionOrder
	if err := platformdb.DB.Where("user_id = ? AND status = ? AND payment_provider IN ? AND purchase_type <> ?", userID, constant.TopUpStatusSuccess, []string{commerceschema.PaymentProviderEpay, commerceschema.PaymentProviderStripe}, commerceschema.SubscriptionPurchaseTypeFuel).Order("id desc").Limit(100).Find(&orders).Error; err != nil {
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
	return refundableTopupTx(platformdb.DB, userID, order)
}

func refundableSubscription(userID int, order *commerceschema.SubscriptionOrder) commerceschema.RefundableOrder {
	return refundableSubscriptionWithDB(platformdb.DB, userID, order)
}

func refundableSubscriptionWithDB(tx *gorm.DB, userID int, order *commerceschema.SubscriptionOrder) commerceschema.RefundableOrder {
	item := commerceschema.RefundableOrder{OrderType: commerceschema.RefundOrderTypeSubscription, TradeNo: order.TradeNo, PaymentMethod: order.PaymentMethod, CreatedAt: order.CreateTime, PaidAmount: order.Money, RefundStatus: order.RefundStatus, RefundNo: order.RefundNo, RefundAmount: order.RefundAmount}
	item.Currency = refundPaymentCurrency(order.PaymentProvider, order.ProviderPayload)
	if order.RefundStatus == commerceschema.RefundStatusFailed || order.RefundStatus == commerceschema.RefundStatusProcessing {
		holdStatus := billingschema.BillingReservationStatusReleased
		if order.RefundStatus == commerceschema.RefundStatusProcessing {
			holdStatus = billingschema.BillingReservationStatusOpen
		}
		if err := validateRefundNativeHoldTx(tx, userID, order, holdStatus); err != nil {
			item.UnavailableReason = err.Error()
			return item
		}
	}
	if order.RefundStatus == commerceschema.RefundStatusProcessing {
		item.RemainingQuota = order.RefundQuota
		item.UnavailableReason = refundStatusReason(order.RefundStatus)
		return item
	}
	if err := validateRefundPayment(order.Status, order.PaymentProvider, order.PaymentMethod, order.ProviderPayload, order.TradeNo, order.Money); err != nil {
		item.UnavailableReason = err.Error()
		return item
	}
	if order.FulfillmentStatus != commerceschema.SubscriptionOrderFulfillmentCompleted || order.PurchaseType == commerceschema.SubscriptionPurchaseTypeFuel {
		item.UnavailableReason = "该套餐订单不支持退款"
		return item
	}

	if order.UserId != userID || order.TargetSubscriptionId <= 0 || order.RefundStatus == commerceschema.RefundStatusSuccess {
		return item
	}
	var paidOrders int64
	if err := tx.Model(&commerceschema.SubscriptionOrder{}).Where("target_subscription_id = ? AND status = ?", order.TargetSubscriptionId, constant.TopUpStatusSuccess).Count(&paidOrders).Error; err != nil || paidOrders > 1 {
		item.UnavailableReason = "同一套餐存在多笔付款，需先核验各订单剩余额度归属"
		return item
	}
	var sub commerceschema.UserSubscription
	if err := tx.Where("id = ? AND user_id = ?", order.TargetSubscriptionId, userID).First(&sub).Error; err != nil {
		item.UnavailableReason = "找不到对应套餐使用记录"
		return item
	}
	if sub.Status != "active" {
		item.UnavailableReason = "套餐当前不可退款"
		return item
	}
	if sub.CreditPackGrantedQuota == 0 {
		var account billingschema.BillingAccount
		err := tx.Where("account_type = ? AND owner_type = ? AND owner_id = ? AND quota_unit = ?", "subscription", "user_subscription", sub.Id, "quota").First(&account).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			item.UnavailableReason = "套餐账本无法核验"
			return item
		}
		if err == nil {
			var snapshot billingschema.BillingBalanceSnapshot
			if err := tx.Where("account_id = ?", account.AccountID).First(&snapshot).Error; err != nil || snapshot.ReservedBalance > 0 {
				item.UnavailableReason = "套餐存在待结算请求"
				return item
			}
		}
	}
	if sub.CreditPackGrantedQuota > 0 {
		var account billingschema.BillingAccount
		if err := tx.Where("account_type = ? AND owner_type = ? AND owner_id = ?", "subscription", "user_subscription", sub.Id).First(&account).Error; err != nil {
			item.UnavailableReason = "找不到额度包账本"
			return item
		}
		var snapshot billingschema.BillingBalanceSnapshot
		if err := tx.Where("account_id = ?", account.AccountID).First(&snapshot).Error; err != nil || snapshot.ReservedBalance > 0 {
			item.UnavailableReason = "额度包存在待结算请求"
			return item
		}
		item.TotalQuota, item.RemainingQuota = sub.CreditPackGrantedQuota, snapshot.AvailableBalance
		item.UsedQuota = max(item.TotalQuota-item.RemainingQuota, 0)
		item.GrossRefund, item.FeeAmount, item.RefundAmount = refundMoney(order.Money, item.RemainingQuota, item.TotalQuota)
		item.Refundable = (order.RefundStatus == "" || order.RefundStatus == commerceschema.RefundStatusFailed) && item.RemainingQuota > 0 && item.RefundAmount >= 0.01 && strings.TrimSpace(extractProviderOrderID(order.ProviderPayload)) != ""
		if !item.Refundable {
			item.UnavailableReason = refundUnavailableReason(order.ProviderPayload, item.RemainingQuota, item.RefundAmount)
		}
		return item
	}
	usedQuota, err := refundableSubscriptionUsedQuotaTx(tx, &sub)
	if err != nil {
		item.UnavailableReason = "套餐已使用额度刷新，无法核验累计消耗，不支持退款"
		return item
	}
	item.TotalQuota = sub.AmountTotal
	item.UsedQuota = usedQuota
	item.RemainingQuota = sub.AmountTotal - usedQuota
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

// refundableSubscriptionUsedQuotaTx preserves consumption erased from the
// subscription projection by a referral reset. Billing snapshots are
// cumulative; ConsumedTotal already contains actual settlement amounts. Excess
// preauthorizations increase RefundedTotal and must not erase real usage.
// If a reset exists but its billing snapshot cannot be
// verified, fail closed instead of treating the refreshed projection as new.
func refundableSubscriptionUsedQuotaTx(tx *gorm.DB, sub *commerceschema.UserSubscription) (int64, error) {
	if tx == nil || sub == nil || sub.Id <= 0 {
		return 0, errors.New("invalid subscription")
	}
	resetUsed, err := subscriptionHasResetOpportunityUsageTx(tx, sub.Id)
	if err != nil || !resetUsed {
		return max(sub.AmountUsed, 0), err
	}

	var account billingschema.BillingAccount
	if err := tx.Select("account_id").
		Where("account_type = ? AND owner_type = ? AND owner_id = ? AND quota_unit = ?", "subscription", "user_subscription", sub.Id, "quota").
		First(&account).Error; err != nil {
		return 0, err
	}
	var snapshot billingschema.BillingBalanceSnapshot
	if err := tx.Where("account_id = ?", account.AccountID).First(&snapshot).Error; err != nil {
		return 0, err
	}
	netConsumed := max(snapshot.ConsumedTotal, 0)
	return max(sub.AmountUsed, netConsumed), nil
}

func refundMoney(paid float64, remaining, total int64) (float64, float64, float64) {
	if paid <= 0 || math.IsNaN(paid) || math.IsInf(paid, 0) || remaining <= 0 || total <= 0 {
		return 0, 0, 0
	}
	remaining = min(remaining, total)
	gross := min(math.Round(paid*float64(remaining)/float64(total)*100)/100, math.Floor(paid*100)/100)
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

// refundOrderState keeps every provider transition bound to the locked local
// payment attempt. Its record is a TopUp or SubscriptionOrder, never user input.
type refundOrderState struct {
	record                                                              any
	subID                                                               int
	paid                                                                float64
	paymentStatus, provider, method, payload, fulfillment, purchaseType string
	status, refundNo, providerID                                        string
	amount                                                              float64
	quota                                                               int64
}

// Old failures can mean a lost provider response, not a confirmed rejection.
// Only this version's exact native hold proves that retrying or advancing an
// attempt is safe. This check never creates accounts, holds, or funding lots.
func validateRefundNativeHoldTx(tx *gorm.DB, userID int, record any, expectedStatus string) error {
	var refundNo, providerID, accountType, ownerType string
	var quota, ownerID int64
	var paid, amount float64
	switch order := record.(type) {
	case *commerceschema.TopUp:
		if order.UserId != userID {
			return errRefundHistoricalReview
		}
		refundNo, providerID, quota, paid, amount = order.RefundNo, order.RefundProviderID, order.RefundQuota, order.Money, order.RefundAmount
		accountType, ownerType, ownerID = "claude_wallet", "user", int64(userID)
	case *commerceschema.SubscriptionOrder:
		if order.UserId != userID || order.TargetSubscriptionId <= 0 {
			return errRefundHistoricalReview
		}
		refundNo, providerID, quota, paid, amount = order.RefundNo, order.RefundProviderID, order.RefundQuota, order.Money, order.RefundAmount
		accountType, ownerType, ownerID = "subscription", "user_subscription", int64(order.TargetSubscriptionId)
	default:
		return errRefundHistoricalReview
	}
	if strings.TrimSpace(refundNo) == "" || quota <= 0 || paid <= 0 || math.IsNaN(paid) || math.IsInf(paid, 0) || amount <= 0 || math.IsNaN(amount) || math.IsInf(amount, 0) || amount > paid ||
		(expectedStatus == billingschema.BillingReservationStatusReleased && strings.TrimSpace(providerID) == "") {
		return errRefundHistoricalReview
	}
	var hold billingschema.BillingReservation
	if err := tx.Where("idempotency_key = ?", "payment-refund:"+refundNo+":reserve").First(&hold).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errRefundHistoricalReview
		}
		return fmt.Errorf("核验退款冻结记录失败: %w", err)
	}
	if hold.Status != expectedStatus || hold.ReservedAmount != quota || hold.WorkflowID != refundNo || hold.RequestID != "" || hold.ExpiresAt != nil {
		return errRefundHistoricalReview
	}
	var account billingschema.BillingAccount
	if err := tx.Where("account_id = ? AND account_type = ? AND owner_type = ? AND owner_id = ? AND quota_unit = ?", hold.AccountID, accountType, ownerType, ownerID, "quota").First(&account).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errRefundHistoricalReview
		}
		return fmt.Errorf("核验退款账户失败: %w", err)
	}
	return nil
}

func loadRefundOrderTx(tx *gorm.DB, userID int, orderType, tradeNo string) (*refundOrderState, error) {
	switch orderType {
	case commerceschema.RefundOrderTypeBalance:
		var o commerceschema.TopUp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND trade_no = ?", userID, tradeNo).First(&o).Error; err != nil {
			return nil, err
		}
		return &refundOrderState{record: &o, paid: o.Money, paymentStatus: o.Status, provider: o.PaymentProvider, method: o.PaymentMethod, payload: o.ProviderPayload, status: o.RefundStatus, refundNo: o.RefundNo, providerID: o.RefundProviderID, amount: o.RefundAmount, quota: o.RefundQuota}, nil
	case commerceschema.RefundOrderTypeSubscription:
		var o commerceschema.SubscriptionOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND trade_no = ?", userID, tradeNo).First(&o).Error; err != nil {
			return nil, err
		}
		return &refundOrderState{record: &o, subID: o.TargetSubscriptionId, paid: o.Money, paymentStatus: o.Status, provider: o.PaymentProvider, method: o.PaymentMethod, payload: o.ProviderPayload, fulfillment: o.FulfillmentStatus, purchaseType: o.PurchaseType, status: o.RefundStatus, refundNo: o.RefundNo, providerID: o.RefundProviderID, amount: o.RefundAmount, quota: o.RefundQuota}, nil
	default:
		return nil, errors.New("不支持的退款订单类型")
	}
}

func validateRefundPayment(status, provider, method, payload, tradeNo string, paid float64) error {
	if provider == commerceschema.PaymentProviderStripe {
		if status != constant.TopUpStatusSuccess || method != commerceschema.PaymentMethodStripe {
			return errors.New("Stripe payment is not refundable")
		}
		_, _, err := validateStripePayment(payload, payload, tradeNo, paid)
		return err
	}

	if status != constant.TopUpStatusSuccess || provider != commerceschema.PaymentProviderEpay || (method != "alipay" && method != "wxpay") || paid <= 0 || math.IsNaN(paid) || math.IsInf(paid, 0) || paid*100 > float64(math.MaxInt64) {
		return errors.New("仅支持已支付成功的现金支付订单退款")
	}
	var data map[string]any
	if json.Unmarshal([]byte(payload), &data) != nil || extractProviderOrderID(payload) == "" {
		return errors.New("缺少可核验的支付平台订单")
	}
	for _, key := range []string{"currency", "Currency", "currency_code"} {
		if value, ok := data[key]; ok && strings.ToUpper(strings.TrimSpace(fmt.Sprint(value))) != "CNY" {
			return errors.New("退款币种无法核验")
		}
	}
	if value, ok := data["out_trade_no"]; ok && fmt.Sprint(value) != tradeNo {
		return errors.New("支付订单号不匹配")
	}
	if value, ok := data["trade_status"]; ok && fmt.Sprint(value) != "TRADE_SUCCESS" {
		return errors.New("支付状态无法核验")
	}
	if value, ok := data["money"]; ok {
		amount, err := strconv.ParseFloat(fmt.Sprint(value), 64)
		if err != nil || math.IsNaN(amount) || math.IsInf(amount, 0) || math.Round(amount*100) != math.Round(paid*100) {
			return errors.New("实付金额无法核验")
		}
	}
	return nil
}

func createRefund(userID int, req commerceschema.RefundRequest) (*commerceschema.RefundResult, error) {
	if userID <= 0 || strings.TrimSpace(req.TradeNo) == "" {
		return nil, errors.New("退款订单参数无效")
	}
	refundNo := fmt.Sprintf("RFUSR%d%s%d", userID, platformruntime.GetRandomString(8), time.Now().Unix())
	var providerOrderID, paymentProvider, paymentPayload string
	var refundAmount float64
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		state, err := loadRefundOrderTx(tx, userID, req.OrderType, req.TradeNo)
		if err != nil {
			return err
		}
		if state.status == commerceschema.RefundStatusFailed {
			if err := validateRefundNativeHoldTx(tx, userID, state.record, billingschema.BillingReservationStatusReleased); err != nil {
				return err
			}
		}
		if err := validateRefundPayment(state.paymentStatus, state.provider, state.method, state.payload, req.TradeNo, state.paid); err != nil {
			return err
		}
		if state.status != "" && state.status != commerceschema.RefundStatusFailed {
			return errors.New(refundStatusReason(state.status))
		}
		var item commerceschema.RefundableOrder
		if req.OrderType == commerceschema.RefundOrderTypeBalance {
			item = refundableTopupTx(tx, userID, state.record.(*commerceschema.TopUp))
			if !item.Refundable {
				return errors.New(item.UnavailableReason)
			}
			if err := billingapp.ReserveTopupRefundTx(tx, userID, req.TradeNo, item.RemainingQuota, refundNo); err != nil {
				return err
			}
		} else {
			if state.fulfillment != commerceschema.SubscriptionOrderFulfillmentCompleted || state.purchaseType == commerceschema.SubscriptionPurchaseTypeFuel {
				return errors.New("该套餐订单不支持退款")
			}
			var sub commerceschema.UserSubscription
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", state.subID, userID).First(&sub).Error; err != nil {
				return err
			}
			if sub.Status != "active" {
				return errors.New("套餐当前不可退款")
			}
			item = refundableSubscriptionTx(tx, userID, state.record.(*commerceschema.SubscriptionOrder))
			if !item.Refundable {
				return errors.New(item.UnavailableReason)
			}
			if err := reserveSubscriptionRefundTx(tx, &sub, item.RemainingQuota, refundNo); err != nil {
				return err
			}
			if err := tx.Model(&sub).Update("status", "refund_pending").Error; err != nil {
				return err
			}
		}
		refundAmount = item.RefundAmount
		providerOrderID = extractProviderOrderID(state.payload)
		paymentProvider, paymentPayload = state.provider, state.payload
		return tx.Model(state.record).Updates(map[string]any{"refund_status": commerceschema.RefundStatusProcessing, "refund_no": refundNo, "refund_amount": refundAmount, "refund_quota": item.RemainingQuota, "refund_provider_id": "", "refund_updated_at": platformruntime.GetTimestamp()}).Error
	})
	if err != nil {
		return nil, err
	}
	if err := invalidateRefundUserCache(userID); err != nil {
		return nil, fmt.Errorf("退款已冻结，缓存刷新失败: %w", err)
	}
	var response *JianPayRefundResult
	if paymentProvider == commerceschema.PaymentProviderStripe {
		response, err = createStripeRefund(paymentPayload, refundNo, int64(math.Round(refundAmount*100)))
	} else {
		response, err = JianPayRefundCreate(providerOrderID, refundNo, int64(math.Round(refundAmount*100)), "管理员审核未使用额度退款（扣除2%手续费）")
	}
	if err != nil {
		// The provider may have received the request. Keep this exact attempt and
		// hold until an administrator queries refundNo; never automatically retry.
		return &commerceschema.RefundResult{OrderType: req.OrderType, TradeNo: req.TradeNo, RefundNo: refundNo, RefundAmount: refundAmount, Status: commerceschema.RefundStatusProcessing}, fmt.Errorf("退款结果待确认，请按原退款单号查询: %w", err)
	}
	if err := saveRefundProviderState(userID, req.OrderType, req.TradeNo, refundNo, response); err != nil {
		return nil, err
	}
	return &commerceschema.RefundResult{OrderType: req.OrderType, TradeNo: req.TradeNo, RefundNo: refundNo, RefundID: response.RefundID, RefundAmount: refundAmount, Status: refundStatusFromJianPay(response.Status)}, nil
}

func reserveSubscriptionRefundTx(tx *gorm.DB, sub *commerceschema.UserSubscription, amount int64, refundNo string) error {
	account, err := billingdomain.EnsureBillingAccountTx(tx, billingdomain.EnsureAccountParams{AccountType: "subscription", OwnerType: "user_subscription", OwnerID: int64(sub.Id), QuotaUnit: "quota"})
	if err != nil {
		return err
	}
	backed, err := subscriptionLedgerHasEntriesTx(tx, account.AccountID)
	if err != nil {
		return err
	}
	if !backed {
		if _, err := billingdomain.CreditAccountTx(tx, billingdomain.CreditAccountParams{AccountID: account.AccountID, Amount: max(sub.AmountTotal-sub.AmountUsed, 0), IdempotencyKey: fmt.Sprintf("subscription-bootstrap:%d", sub.Id), ReasonCode: "subscription_balance_bootstrap", ReferenceType: "user_subscription", ReferenceID: fmt.Sprint(sub.Id), OperatorType: "subscription_projection", OperatorID: refundNo}); err != nil {
			return err
		}
	}
	var snapshot billingschema.BillingBalanceSnapshot
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("account_id = ?", account.AccountID).First(&snapshot).Error; err != nil {
		return err
	}
	if amount <= 0 || snapshot.ReservedBalance != 0 || snapshot.AvailableBalance < amount {
		return errors.New("套餐存在待结算请求或可退款余额不足")
	}
	_, err = billingdomain.CreateReservationTx(tx, billingdomain.CreateReservationParams{AccountID: account.AccountID, WorkflowID: refundNo, ReservedAmount: amount, IdempotencyKey: "payment-refund:" + refundNo + ":reserve"})
	return err
}

func completeSubscriptionRefundTx(tx *gorm.DB, subID int, amount int64, refundNo string, success bool) error {
	var account billingschema.BillingAccount
	if err := tx.Where("account_type = ? AND owner_type = ? AND owner_id = ? AND quota_unit = ?", "subscription", "user_subscription", subID, "quota").First(&account).Error; err != nil {
		return err
	}
	var reservation billingschema.BillingReservation
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("idempotency_key = ?", "payment-refund:"+refundNo+":reserve").First(&reservation).Error; err != nil {
		return errors.New("退款冻结记录缺失，需管理员核查历史退款")
	}
	if reservation.AccountID != account.AccountID || reservation.ReservedAmount != amount || reservation.WorkflowID != refundNo || reservation.RequestID != "" {
		return errors.New("退款冻结记录不匹配")
	}
	if success {
		_, err := billingdomain.SettleReservationTx(tx, billingdomain.SettleReservationParams{ReservationID: reservation.ReservationID, ActualAmount: amount, IdempotencyKey: "payment-refund:" + refundNo + ":settle"})
		return err
	}
	_, err := billingdomain.ReleaseReservationTx(tx, billingdomain.ReleaseReservationParams{ReservationID: reservation.ReservationID, IdempotencyKey: "payment-refund:" + refundNo + ":release", ReasonCode: "payment_refund_failed"})
	return err
}

func syncRefund(userID int, refundNo string) (*commerceschema.RefundResult, error) {
	if userID <= 0 || strings.TrimSpace(refundNo) == "" {
		return nil, errors.New("退款查询参数无效")
	}
	var topup commerceschema.TopUp
	err := platformdb.DB.Where("user_id = ? AND refund_no = ?", userID, refundNo).First(&topup).Error
	if err == nil {
		return syncRefundOrder(userID, commerceschema.RefundOrderTypeBalance, topup.TradeNo, refundNo, topup.RefundProviderID, topup.RefundAmount)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	var order commerceschema.SubscriptionOrder
	if err := platformdb.DB.Where("user_id = ? AND refund_no = ?", userID, refundNo).First(&order).Error; err != nil {
		return nil, err
	}
	return syncRefundOrder(userID, commerceschema.RefundOrderTypeSubscription, order.TradeNo, refundNo, order.RefundProviderID, order.RefundAmount)
}

func syncRefundOrder(userID int, orderType, tradeNo, refundNo, providerID string, amount float64) (*commerceschema.RefundResult, error) {
	var terminal, paymentProvider, paymentPayload string
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		state, err := loadRefundOrderTx(tx, userID, orderType, tradeNo)
		if err != nil {
			return err
		}
		paymentProvider, paymentPayload = state.provider, state.payload
		if state.refundNo != refundNo || state.amount != amount {
			return errors.New("退款尝试已变更")
		}
		if state.status == commerceschema.RefundStatusFailed || state.status == commerceschema.RefundStatusProcessing {
			holdStatus := billingschema.BillingReservationStatusReleased
			if state.status == commerceschema.RefundStatusProcessing {
				holdStatus = billingschema.BillingReservationStatusOpen
			}
			if err := validateRefundNativeHoldTx(tx, userID, state.record, holdStatus); err != nil {
				return err
			}
		}
		if state.status == commerceschema.RefundStatusSuccess || state.status == commerceschema.RefundStatusFailed {
			terminal = state.status
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if terminal != "" {
		return &commerceschema.RefundResult{OrderType: orderType, TradeNo: tradeNo, RefundNo: refundNo, RefundID: providerID, RefundAmount: amount, Status: terminal}, nil
	}
	var response *JianPayRefundResult
	if paymentProvider == commerceschema.PaymentProviderStripe {
		response, err = queryStripeRefund(paymentPayload, providerID, refundNo, int64(math.Round(amount*100)))
	} else {
		response, err = JianPayRefundQuery(providerID, refundNo)
	}
	if err != nil {
		return nil, err
	}
	if err := saveRefundProviderState(userID, orderType, tradeNo, refundNo, response); err != nil {
		return nil, err
	}
	return &commerceschema.RefundResult{OrderType: orderType, TradeNo: tradeNo, RefundNo: refundNo, RefundID: response.RefundID, RefundAmount: amount, Status: refundStatusFromJianPay(response.Status)}, nil
}

func finalizeUserRefund(userID int, orderType, tradeNo, refundNo string, refundAmount float64) error {
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		state, err := loadRefundOrderTx(tx, userID, orderType, tradeNo)
		if err != nil {
			return err
		}
		return finalizeRefundTx(tx, userID, orderType, tradeNo, refundNo, refundAmount, state)
	})
	if err != nil {
		return err
	}
	return invalidateRefundUserCache(userID)
}

func finalizeRefundTx(tx *gorm.DB, userID int, orderType, tradeNo, refundNo string, refundAmount float64, state *refundOrderState) error {
	if state.refundNo != refundNo || refundNo == "" || state.amount != refundAmount {
		return errors.New("退款尝试或金额不匹配")
	}
	if state.status == commerceschema.RefundStatusSuccess {
		return nil
	}
	if state.status != commerceschema.RefundStatusProcessing || state.quota <= 0 {
		return errors.New("退款状态无效")
	}
	if orderType == commerceschema.RefundOrderTypeBalance {
		if err := billingapp.CompleteTopupRefundTx(tx, userID, tradeNo, state.quota, refundNo, true); err != nil {
			return err
		}
	} else {
		var sub commerceschema.UserSubscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", state.subID, userID).First(&sub).Error; err != nil {
			return err
		}
		if sub.Status != "refund_pending" || sub.AmountTotal-sub.AmountUsed < state.quota {
			return errors.New("套餐冻结状态或额度不匹配")
		}
		if err := completeSubscriptionRefundTx(tx, sub.Id, state.quota, refundNo, true); err != nil {
			return err
		}
		sub.AmountTotal -= state.quota
		sub.PeriodAmount -= minInt64(state.quota, max(sub.PeriodAmount-sub.PeriodUsed, 0))
		sub.Status = commerceschema.SubscriptionStatusSettledCancelled
		if err := tx.Save(&sub).Error; err != nil {
			return err
		}
	}
	return tx.Model(state.record).Updates(map[string]any{"refund_status": commerceschema.RefundStatusSuccess, "refund_updated_at": platformruntime.GetTimestamp()}).Error
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}

func refundableTopupTx(tx *gorm.DB, userID int, order *commerceschema.TopUp) commerceschema.RefundableOrder {
	item := commerceschema.RefundableOrder{OrderType: commerceschema.RefundOrderTypeBalance, TradeNo: order.TradeNo, PaymentMethod: order.PaymentMethod, CreatedAt: order.CreateTime, PaidAmount: order.Money, RefundStatus: order.RefundStatus, RefundNo: order.RefundNo, RefundAmount: order.RefundAmount}
	item.Currency = refundPaymentCurrency(order.PaymentProvider, order.ProviderPayload)
	if order.RefundStatus == commerceschema.RefundStatusFailed || order.RefundStatus == commerceschema.RefundStatusProcessing {
		holdStatus := billingschema.BillingReservationStatusReleased
		if order.RefundStatus == commerceschema.RefundStatusProcessing {
			holdStatus = billingschema.BillingReservationStatusOpen
		}
		if err := validateRefundNativeHoldTx(tx, userID, order, holdStatus); err != nil {
			item.UnavailableReason = err.Error()
			return item
		}
	}
	if order.RefundStatus == commerceschema.RefundStatusProcessing {
		item.RemainingQuota = order.RefundQuota
		item.UnavailableReason = refundStatusReason(order.RefundStatus)
		return item
	}
	if err := validateRefundPayment(order.Status, order.PaymentProvider, order.PaymentMethod, order.ProviderPayload, order.TradeNo, order.Money); err != nil {
		item.UnavailableReason = err.Error()
		return item
	}
	original, remaining, err := billingapp.TopupFundingLotAmounts(tx, userID, order.TradeNo)
	if err != nil {
		item.UnavailableReason = "该订单没有可识别的充值余额"
		return item
	}
	item.TotalQuota, item.RemainingQuota, item.UsedQuota = original, remaining, max(original-remaining, 0)
	item.GrossRefund, item.FeeAmount, item.RefundAmount = refundMoney(order.Money, remaining, original)
	item.Refundable = (order.RefundStatus == "" || order.RefundStatus == commerceschema.RefundStatusFailed) && remaining > 0 && item.RefundAmount >= 0.01
	if !item.Refundable {
		item.UnavailableReason = refundStatusReason(order.RefundStatus)
		if item.UnavailableReason == "" {
			item.UnavailableReason = refundUnavailableReason(order.ProviderPayload, remaining, item.RefundAmount)
		}
	}
	return item
}

func refundableSubscriptionTx(tx *gorm.DB, userID int, order *commerceschema.SubscriptionOrder) commerceschema.RefundableOrder {
	return refundableSubscriptionWithDB(tx, userID, order)
}

func saveRefundProviderState(userID int, orderType, tradeNo, refundNo string, response *JianPayRefundResult) error {
	if response == nil {
		return errors.New("退款平台响应为空")
	}
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		state, err := loadRefundOrderTx(tx, userID, orderType, tradeNo)
		if err != nil {
			return err
		}
		if state.refundNo != refundNo || response.RefundNo != refundNo || response.OrderID != extractProviderOrderID(state.payload) || response.RefundAmount != int64(math.Round(state.amount*100)) || state.amount <= 0 || state.amount > state.paid || response.RefundID == "" || (state.providerID != "" && state.providerID != response.RefundID) {
			return errors.New("退款平台订单或金额不匹配")
		}
		if state.status == commerceschema.RefundStatusSuccess {
			return nil
		}
		if state.status != commerceschema.RefundStatusProcessing {
			return errors.New("退款状态已变更")
		}
		if err := validateRefundNativeHoldTx(tx, userID, state.record, billingschema.BillingReservationStatusOpen); err != nil {
			return err
		}
		if err := tx.Model(state.record).Update("refund_provider_id", response.RefundID).Error; err != nil {
			return err
		}
		switch response.Status {
		case 2:
			return finalizeRefundTx(tx, userID, orderType, tradeNo, refundNo, state.amount, state)
		case 3:
			return failRefundTx(tx, userID, orderType, tradeNo, refundNo, state)
		default:
			return tx.Model(state.record).Update("refund_updated_at", platformruntime.GetTimestamp()).Error
		}
	})
	if err != nil {
		return err
	}
	return invalidateRefundUserCache(userID)
}

func failRefundTx(tx *gorm.DB, userID int, orderType, tradeNo, refundNo string, state *refundOrderState) error {
	if state.refundNo != refundNo || state.status != commerceschema.RefundStatusProcessing {
		return errors.New("退款状态已变更")
	}
	if orderType == commerceschema.RefundOrderTypeBalance {
		if err := billingapp.CompleteTopupRefundTx(tx, userID, tradeNo, state.quota, refundNo, false); err != nil {
			return err
		}
	} else {
		var sub commerceschema.UserSubscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", state.subID, userID).First(&sub).Error; err != nil {
			return err
		}
		if sub.Status != "refund_pending" {
			return errors.New("套餐退款冻结状态不匹配")
		}
		if err := completeSubscriptionRefundTx(tx, sub.Id, state.quota, refundNo, false); err != nil {
			return err
		}
		if err := tx.Model(&sub).Update("status", "active").Error; err != nil {
			return err
		}
	}
	return tx.Model(state.record).Updates(map[string]any{"refund_status": commerceschema.RefundStatusFailed, "refund_updated_at": platformruntime.GetTimestamp()}).Error
}

func invalidateRefundUserCache(userID int) error {
	return errors.Join(identitystore.InvalidateUserCache(userID), identitystore.InvalidateUserTokensCache(userID))
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
	for _, key := range []string{"trade_no", "TradeNo", "orderId", "order_id", "payment_intent"} {
		if value, ok := data[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
