package schema

import (
	"strings"

	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

const (
	BlindBoxPropTypeTopupDiscount90        = "topup_discount_90"
	BlindBoxPropTypeSubscriptionDiscount90 = "subscription_discount_90"
	BlindBoxPropTypeConsumeDiscount95      = "consume_discount_95"
	BlindBoxPropTypeConsumeDiscount90      = "consume_discount_90"
	BlindBoxPropTypeZeroHourMultiplier     = "zero_hour_multiplier"
)

const (
	BlindBoxPropStatusAvailable = "available"
	BlindBoxPropStatusActive    = "active"
	BlindBoxPropStatusReserved  = "reserved"
	BlindBoxPropStatusUsed      = "used"
	BlindBoxPropStatusExpired   = "expired"
)

const (
	BlindBoxPropOrderTypeTopup        = "topup"
	BlindBoxPropOrderTypeSubscription = "subscription"
)

type BlindBoxProp struct {
	Id int `json:"id"`

	UserId       int    `json:"user_id" gorm:"index"`
	OpenRecordId int    `json:"open_record_id" gorm:"index"`
	PropType     string `json:"prop_type" gorm:"type:varchar(64);index"`
	Title        string `json:"title" gorm:"type:varchar(255)"`
	Status       string `json:"status" gorm:"type:varchar(32);index"`

	DiscountRate float64 `json:"discount_rate" gorm:"type:decimal(8,4);not null;default:0"`
	Multiplier   float64 `json:"multiplier" gorm:"type:decimal(8,4);not null;default:1"`

	DurationSeconds int64 `json:"duration_seconds" gorm:"bigint;not null;default:0"`
	ActivatedAt     int64 `json:"activated_at" gorm:"bigint;index;default:0"`
	ExpiresAt       int64 `json:"expires_at" gorm:"bigint;index;default:0"`
	ReservedAt      int64 `json:"reserved_at" gorm:"bigint;default:0"`
	UsedAt          int64 `json:"used_at" gorm:"bigint;default:0"`

	ReservedOrderType    string `json:"reserved_order_type" gorm:"type:varchar(32);index;default:''"`
	ReservedOrderTradeNo string `json:"reserved_order_trade_no" gorm:"type:varchar(255);index;default:''"`

	CreatedAt int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt int64 `json:"updated_at" gorm:"bigint"`
}

// BlindBoxZeroHourState tracks one user's hidden zero-multiplier draw progress.
// Points are deliberately integer based to keep probability updates deterministic.
type BlindBoxZeroHourState struct {
	Id         int   `json:"id"`
	UserId     int   `json:"user_id" gorm:"uniqueIndex"`
	Points     int64 `json:"points" gorm:"not null;default:0"`
	UsageQuota int64 `json:"usage_quota" gorm:"not null;default:0"`
	HitCount   int   `json:"hit_count" gorm:"not null;default:0"`
	UpdatedAt  int64 `json:"updated_at" gorm:"bigint"`
}

type BlindBoxPropSpec struct {
	PropType        string
	Title           string
	DiscountRate    float64
	Multiplier      float64
	DurationSeconds int64
	Activatable     bool
}

func (p *BlindBoxProp) BeforeCreate(tx *gorm.DB) error {
	now := platformruntime.GetTimestamp()
	p.CreatedAt = now
	p.UpdatedAt = now
	if strings.TrimSpace(p.Status) == "" {
		p.Status = BlindBoxPropStatusAvailable
	}
	return nil
}

func (p *BlindBoxProp) BeforeUpdate(tx *gorm.DB) error {
	p.UpdatedAt = platformruntime.GetTimestamp()
	return nil
}
