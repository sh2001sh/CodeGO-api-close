package schema

import (
	"errors"
	"strings"

	"gorm.io/gorm"
)

const (
	PaymentMethodStripe       = "stripe"
	PaymentMethodCreem        = "creem"
	PaymentMethodWaffo        = "waffo"
	PaymentMethodWaffoPancake = "waffo_pancake"
	PaymentMethodXunhu        = "xunhu"

	PaymentProviderEpay         = "epay"
	PaymentProviderStripe       = "stripe"
	PaymentProviderCreem        = "creem"
	PaymentProviderWaffo        = "waffo"
	PaymentProviderWaffoPancake = "waffo_pancake"
	PaymentProviderXunhu        = "xunhu"

	WalletTypeDefault = "default"
	WalletTypeClaude  = "claude"
)

var (
	ErrPaymentMethodMismatch = errors.New("payment method mismatch")
	ErrTopUpNotFound         = errors.New("topup not found")
	ErrTopUpStatusInvalid    = errors.New("topup status invalid")
)

// TopUp is a commerce payment order.
type TopUp struct {
	Id                              int     `json:"id"`
	UserId                          int     `json:"user_id" gorm:"index"`
	Amount                          int64   `json:"amount"`
	Money                           float64 `json:"money"`
	TradeNo                         string  `json:"trade_no" gorm:"unique;type:varchar(255);index"`
	PaymentMethod                   string  `json:"payment_method" gorm:"type:varchar(50)"`
	PaymentProvider                 string  `json:"payment_provider" gorm:"type:varchar(50);default:''"`
	WalletType                      string  `json:"wallet_type" gorm:"type:varchar(32);default:'default';index"`
	FirstPurchaseDiscountApplied    bool    `json:"first_purchase_discount_applied" gorm:"not null;default:false;index"`
	FirstPurchaseDiscountMultiplier float64 `json:"first_purchase_discount_multiplier" gorm:"type:decimal(8,4);not null;default:0"`
	CreateTime                      int64   `json:"create_time"`
	CompleteTime                    int64   `json:"complete_time"`
	Status                          string  `json:"status"`
}

func NormalizeWalletType(walletType string) string {
	if strings.EqualFold(strings.TrimSpace(walletType), WalletTypeClaude) {
		return WalletTypeClaude
	}
	return WalletTypeDefault
}

func (topUp *TopUp) NormalizedWalletType() string {
	if topUp == nil {
		return WalletTypeDefault
	}
	return NormalizeWalletType(topUp.WalletType)
}

func (topUp *TopUp) BeforeCreate(_ *gorm.DB) error {
	topUp.WalletType = NormalizeWalletType(topUp.WalletType)
	return nil
}

func (topUp *TopUp) BeforeUpdate(_ *gorm.DB) error {
	topUp.WalletType = NormalizeWalletType(topUp.WalletType)
	return nil
}
