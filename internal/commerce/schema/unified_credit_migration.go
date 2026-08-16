package schema

const (
	UnifiedCreditMigrationVersion = "unified_credit_v1"
	UnifiedCreditMigrationPending = "pending"
	UnifiedCreditMigrationApplied = "applied"
	UnifiedCreditMigrationReview  = "review_required"
)

// UnifiedCreditUserMigration is the per-user idempotency and audit record.
type UnifiedCreditUserMigration struct {
	Id int `json:"id"`

	UserId                   int    `json:"user_id" gorm:"not null;uniqueIndex"`
	Version                  string `json:"version" gorm:"type:varchar(32);not null;index"`
	LegacyGPTQuota           int64  `json:"legacy_gpt_quota" gorm:"type:bigint;not null;default:0"`
	ConvertedUnifiedQuota    int64  `json:"converted_unified_quota" gorm:"type:bigint;not null;default:0"`
	SubscriptionUnifiedQuota int64  `json:"subscription_unified_quota" gorm:"type:bigint;not null;default:0"`
	Status                   string `json:"status" gorm:"type:varchar(32);not null;index"`
	ReviewReason             string `json:"review_reason" gorm:"type:text"`
	CreatedAt                int64  `json:"created_at" gorm:"type:bigint;not null"`
	CompletedAt              int64  `json:"completed_at" gorm:"type:bigint;not null;default:0"`
}

// SubscriptionTierSettlement preserves the exact inputs and result of one card settlement.
type SubscriptionTierSettlement struct {
	Id int `json:"id"`

	UserSubscriptionId int    `json:"user_subscription_id" gorm:"not null;uniqueIndex"`
	UserId             int    `json:"user_id" gorm:"not null;index"`
	PlanId             int    `json:"plan_id" gorm:"not null;index"`
	MembershipTier     string `json:"membership_tier" gorm:"type:varchar(16);not null;index"`
	BasePriceCents     int64  `json:"base_price_cents" gorm:"type:bigint;not null"`
	AmountTotal        int64  `json:"amount_total" gorm:"type:bigint;not null"`
	AmountUsed         int64  `json:"amount_used" gorm:"type:bigint;not null"`
	UnusedAmount       int64  `json:"unused_amount" gorm:"type:bigint;not null"`
	SettlementQuota    int64  `json:"settlement_quota" gorm:"type:bigint;not null"`
	RuleVersion        string `json:"rule_version" gorm:"type:varchar(32);not null"`
	Status             string `json:"status" gorm:"type:varchar(32);not null;index"`
	ReviewReason       string `json:"review_reason" gorm:"type:text"`
	CreatedAt          int64  `json:"created_at" gorm:"type:bigint;not null"`
	SettledAt          int64  `json:"settled_at" gorm:"type:bigint;not null;default:0"`
}

// UnifiedCreditGroupRatioMigration audits the one-time GPT group multiplier
// adjustment required to preserve purchasing power after quota conversion.
type UnifiedCreditGroupRatioMigration struct {
	Id int `json:"id"`

	Version     string  `json:"version" gorm:"type:varchar(32);not null;uniqueIndex:uq_unified_group_ratio,priority:1"`
	GroupName   string  `json:"group_name" gorm:"type:varchar(64);not null;uniqueIndex:uq_unified_group_ratio,priority:2"`
	RatioBefore float64 `json:"ratio_before" gorm:"type:decimal(12,6);not null"`
	RatioAfter  float64 `json:"ratio_after" gorm:"type:decimal(12,6);not null"`
	CreatedAt   int64   `json:"created_at" gorm:"bigint;not null"`
}
