package schema

import (
	"errors"

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
	ExternalPaymentID               string  `json:"external_payment_id,omitempty" gorm:"type:varchar(128);index"`
	ProviderPayload                 string  `json:"provider_payload,omitempty" gorm:"type:text"`
	WalletType                      string  `json:"wallet_type" gorm:"type:varchar(32);default:'claude';index"`
	FirstPurchaseDiscountApplied    bool    `json:"first_purchase_discount_applied" gorm:"not null;default:false;index"`
	FirstPurchaseDiscountMultiplier float64 `json:"first_purchase_discount_multiplier" gorm:"type:decimal(8,4);not null;default:0"`
	CreateTime                      int64   `json:"create_time" gorm:"index:idx_topups_pending_created,priority:2"`
	CompleteTime                    int64   `json:"complete_time"`
	Status                          string  `json:"status" gorm:"index:idx_topups_pending_created,priority:1"`
	RefundStatus                    string  `json:"refund_status" gorm:"type:varchar(24);default:'';index"`
	RefundNo                        string  `json:"refund_no,omitempty" gorm:"type:varchar(128);index"`
	RefundProviderID                string  `json:"refund_provider_id,omitempty" gorm:"type:varchar(128)"`
	RefundAmount                    float64 `json:"refund_amount" gorm:"type:decimal(10,2);default:0"`
	RefundQuota                     int64   `json:"refund_quota" gorm:"default:0"`
	RefundUpdatedAt                 int64   `json:"refund_updated_at"`
}

func NormalizeWalletType(walletType string) string {
	return WalletTypeClaude
}

func (topUp *TopUp) NormalizedWalletType() string {
	if topUp == nil {
		return WalletTypeClaude
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
