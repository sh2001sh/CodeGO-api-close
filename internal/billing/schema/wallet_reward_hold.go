package billingschema

import (
	"strings"
	"time"

	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

// WalletRewardHold tracks blind-box quota that is usable by the owner but not
// yet eligible for peer transfer. Release eligibility is calculated from the
// account age so no background job is required.
type WalletRewardHold struct {
	HoldID         string    `json:"hold_id" gorm:"column:hold_id;primaryKey;size:64"`
	AccountID      string    `json:"account_id" gorm:"column:account_id;size:64;index"`
	UserID         int       `json:"user_id" gorm:"column:user_id;index"`
	OriginalAmount int64     `json:"original_amount" gorm:"column:original_amount"`
	ConsumedAmount int64     `json:"consumed_amount" gorm:"column:consumed_amount;default:0"`
	ReferenceType  string    `json:"reference_type" gorm:"column:reference_type;size:32"`
	ReferenceID    string    `json:"reference_id" gorm:"column:reference_id;size:128"`
	IdempotencyKey string    `json:"-" gorm:"column:idempotency_key;size:255;uniqueIndex"`
	CreatedAt      time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime;index"`
}

func (WalletRewardHold) TableName() string {
	if platformdb.UsingPostgreSQL {
		return "billing.wallet_reward_holds"
	}
	return "billing_wallet_reward_holds"
}

func (hold *WalletRewardHold) BeforeCreate(_ *gorm.DB) error {
	if strings.TrimSpace(hold.HoldID) == "" {
		hold.HoldID = platformruntime.GetUUID()
	}
	return nil
}
