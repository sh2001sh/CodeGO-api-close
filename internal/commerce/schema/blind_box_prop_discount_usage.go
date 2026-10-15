package schema

// BlindBoxPropDiscountUsage is the idempotent audit record for one request's
// multiplier-card discount.
type BlindBoxPropDiscountUsage struct {
	Id int `json:"id"`

	RequestId    string `json:"request_id" gorm:"type:varchar(128);uniqueIndex;not null"`
	UserId       int    `json:"user_id" gorm:"index;not null"`
	PropId       int    `json:"prop_id" gorm:"index;not null"`
	PropTitle    string `json:"prop_title" gorm:"type:varchar(255);not null"`
	ChannelId    int    `json:"channel_id" gorm:"index;not null"`
	ChannelScope string `json:"channel_scope" gorm:"type:varchar(16);not null"`
	ModelName    string `json:"model_name" gorm:"type:varchar(191);not null"`

	QuotaBeforeDiscount int64   `json:"quota_before_discount" gorm:"bigint;not null"`
	QuotaAfterDiscount  int64   `json:"quota_after_discount" gorm:"bigint;not null"`
	DiscountQuota       int64   `json:"discount_quota" gorm:"bigint;not null"`
	DiscountRate        float64 `json:"discount_rate" gorm:"type:decimal(8,4);not null"`
	// Multiplier is the configured/nominal card multiplier. The effective
	// multiplier can differ slightly because quota is settled in integers.
	Multiplier          float64 `json:"multiplier" gorm:"type:decimal(8,4);not null"`
	EffectiveMultiplier float64 `json:"effective_multiplier" gorm:"column:effective_multiplier;type:decimal(8,4);not null;default:1"`
	RemainingQuota      int64   `json:"remaining_quota" gorm:"bigint;not null"`
	CreatedAt           int64   `json:"created_at" gorm:"bigint;index"`
}
