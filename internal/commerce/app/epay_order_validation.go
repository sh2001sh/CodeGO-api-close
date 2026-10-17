package app

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/Calcium-Ion/go-epay/epay"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func verifyConfiguredEpayOrder(params map[string]string) (*epay.VerifyRes, error) {
	client := GetEpayClient()
	if client == nil {
		return nil, errors.New("payment gateway is not configured")
	}
	// The SDK mutates sign/sign_type while verifying; keep rejected input intact.
	copyParams := make(map[string]string, len(params))
	for key, value := range params {
		copyParams[key] = value
	}
	verified, err := client.Verify(copyParams)
	if err != nil || verified == nil || !verified.VerifyStatus {
		return nil, errors.New("epay verification failed")
	}
	if params["pid"] != client.Config.PartnerID {
		return nil, errors.New("epay merchant mismatch")
	}
	if currency := params["currency"]; currency != "" && currency != "CNY" {
		return nil, errors.New("epay currency mismatch")
	}
	return verified, nil
}

func validateEpayOrderPayment(payload, tradeNo, paymentMethod string, money float64) (*epay.VerifyRes, error) {
	var paid epay.VerifyRes
	if json.Unmarshal([]byte(payload), &paid) != nil || !paid.VerifyStatus || paid.TradeStatus != epay.StatusTradeSuccess ||
		paid.ServiceTradeNo != tradeNo || strings.TrimSpace(paid.TradeNo) == "" || paymentMethod == "" || paid.Type != paymentMethod {
		return nil, errors.New("epay original payment mismatch")
	}
	amount, err := decimal.NewFromString(paid.Money)
	if err != nil || !amount.IsPositive() || !amount.Equal(amount.Round(2)) || math.IsNaN(money) || math.IsInf(money, 0) || money <= 0 {
		return nil, errors.New("invalid epay payment amount")
	}
	expected, err := decimal.NewFromString(strconv.FormatFloat(money, 'f', 2, 64))
	if err != nil || !expected.Equal(amount) {
		return nil, errors.New("epay payment amount mismatch")
	}
	return &paid, nil
}

func snapshotSubscriptionEpayCheckout(order *commerceschema.SubscriptionOrder, propID int) error {
	if order.PaymentProvider != commerceschema.PaymentProviderEpay {
		return nil
	}
	payload := map[string]any{}
	if order.ProviderPayload != "" {
		if err := json.Unmarshal([]byte(order.ProviderPayload), &payload); err != nil {
			return err
		}
		if payload == nil {
			return errors.New("invalid epay checkout payload")
		}
	}
	payload["epay_checkout"] = epayTopupCheckoutSnapshot{Version: 1, PropID: propID}
	data, err := json.Marshal(payload)
	if err == nil {
		order.ProviderPayload = string(data)
	}
	return err
}

func consumeSubscriptionEpayOriginalPropTx(tx *gorm.DB, order *commerceschema.SubscriptionOrder) error {
	var envelope struct {
		Checkout *epayTopupCheckoutSnapshot `json:"epay_checkout"`
	}
	if order.ProviderPayload != "" && json.Unmarshal([]byte(order.ProviderPayload), &envelope) != nil {
		return errors.New("invalid epay checkout evidence")
	}
	var props []commerceschema.BlindBoxProp
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("reserved_order_trade_no = ? AND reserved_order_type = ? AND status = ?", order.TradeNo, commerceschema.BlindBoxPropOrderTypeSubscription, commerceschema.BlindBoxPropStatusReserved).Limit(2).Find(&props).Error; err != nil {
		return err
	}
	if len(props) > 1 {
		return errors.New("epay discount reservation conflict")
	}
	if envelope.Checkout != nil {
		evidence := envelope.Checkout
		if evidence.Version != 1 || evidence.PropID < 0 || (evidence.PropID == 0 && len(props) != 0) ||
			(evidence.PropID > 0 && (len(props) != 1 || props[0].Id != evidence.PropID)) {
			return errors.New("epay original discount prop released or conflicting; manual review required")
		}
	} else if len(props) == 0 {
		// Permanent packs are created exclusively through a no-card branch;
		// their frozen pack fields provide historical evidence of that branch.
		if order.CreditPackQuota <= 0 || order.CreditPackPaidQuota <= 0 {
			return errors.New("epay original discount evidence missing; manual review required")
		}
	}
	if len(props) == 0 {
		return nil
	}
	prop := props[0]
	if prop.UserId != order.UserId || (prop.PropType != commerceschema.BlindBoxPropTypeTopupDiscount90 && prop.PropType != commerceschema.BlindBoxPropTypeSubscriptionDiscount90) {
		return errors.New("epay original discount prop ownership mismatch")
	}
	now := platformruntime.GetTimestamp()
	result := tx.Model(&commerceschema.BlindBoxProp{}).Where("id = ? AND user_id = ? AND status = ? AND reserved_order_type = ? AND reserved_order_trade_no = ?", prop.Id, order.UserId, commerceschema.BlindBoxPropStatusReserved, commerceschema.BlindBoxPropOrderTypeSubscription, order.TradeNo).
		Updates(map[string]any{"status": commerceschema.BlindBoxPropStatusUsed, "used_at": now, "reserved_order_type": "", "reserved_order_trade_no": "", "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("epay original discount prop changed")
	}
	return nil
}
