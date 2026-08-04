package schema

import platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
import "gorm.io/gorm"

const (
	SubscriptionMembershipTierNone     = "none"
	SubscriptionMembershipTierLite     = "lite"
	SubscriptionMembershipTierStandard = "standard"
	SubscriptionMembershipTierPro      = "pro"
	SubscriptionMembershipTierUltra    = "ultra"
)

const (
	SubscriptionLuckyDrawStatusPending   = "pending"
	SubscriptionLuckyDrawStatusSettling  = "settling"
	SubscriptionLuckyDrawStatusCompleted = "completed"
	SubscriptionLuckyDrawStatusFailed    = "failed"
)

const (
	SubscriptionLuckyRewardCreditPending  = "pending"
	SubscriptionLuckyRewardCreditCredited = "credited"
	SubscriptionLuckyRewardCreditFailed   = "failed"
)

const (
	SubscriptionLuckyBenefitStatusPending   = "pending"
	SubscriptionLuckyBenefitStatusCompleted = "completed"
	SubscriptionLuckyBenefitStatusFailed    = "failed"
)

// SubscriptionLuckyNumber is the permanent public identifier of a subscription.
type SubscriptionLuckyNumber struct {
	Id                 int    `json:"id"`
	UserSubscriptionId int    `json:"user_subscription_id" gorm:"uniqueIndex"`
	UserId             int    `json:"user_id" gorm:"index"`
	CardCode           string `json:"card_code" gorm:"type:varchar(32);uniqueIndex"`
	LuckySuffix        string `json:"lucky_suffix" gorm:"type:char(4);index"`
	AssignedAt         int64  `json:"assigned_at" gorm:"bigint"`
	CreatedAt          int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt          int64  `json:"updated_at" gorm:"bigint"`
}

func (n *SubscriptionLuckyNumber) BeforeCreate(_ *gorm.DB) error {
	now := platformruntime.GetTimestamp()
	n.AssignedAt = now
	n.CreatedAt = now
	n.UpdatedAt = now
	return nil
}

func (n *SubscriptionLuckyNumber) BeforeUpdate(_ *gorm.DB) error {
	n.UpdatedAt = platformruntime.GetTimestamp()
	return nil
}

// SubscriptionLuckyDraw stores an immutable daily draw and its configuration snapshot.
type SubscriptionLuckyDraw struct {
	Id                  int     `json:"id"`
	DrawDate            string  `json:"draw_date" gorm:"type:char(10);uniqueIndex"`
	WinningNumber       string  `json:"winning_number" gorm:"type:char(4)"`
	JackpotBefore       float64 `json:"jackpot_before" gorm:"type:decimal(12,2);not null;default:100"`
	JackpotAfter        float64 `json:"jackpot_after" gorm:"type:decimal(12,2);not null;default:100"`
	FullMatchCount      int     `json:"full_match_count" gorm:"type:int;not null;default:0"`
	Status              string  `json:"status" gorm:"type:varchar(16);index"`
	ErrorMessage        string  `json:"error_message,omitempty" gorm:"type:varchar(512)"`
	Timezone            string  `json:"timezone" gorm:"type:varchar(64);not null;default:'Asia/Shanghai'"`
	DrawHour            int     `json:"draw_hour" gorm:"type:int;not null;default:20"`
	DrawMinute          int     `json:"draw_minute" gorm:"type:int;not null;default:0"`
	BaseReward1USD      float64 `json:"base_reward_1_usd" gorm:"type:decimal(12,2);not null;default:1"`
	BaseReward2USD      float64 `json:"base_reward_2_usd" gorm:"type:decimal(12,2);not null;default:10"`
	BaseReward3USD      float64 `json:"base_reward_3_usd" gorm:"type:decimal(12,2);not null;default:50"`
	BaseReward4USD      float64 `json:"base_reward_4_usd" gorm:"type:decimal(12,2);not null;default:100"`
	MultiplierLite      float64 `json:"multiplier_lite" gorm:"type:decimal(8,4);not null;default:1"`
	MultiplierStandard  float64 `json:"multiplier_standard" gorm:"type:decimal(8,4);not null;default:1.1"`
	MultiplierPro       float64 `json:"multiplier_pro" gorm:"type:decimal(8,4);not null;default:1.2"`
	MultiplierUltra     float64 `json:"multiplier_ultra" gorm:"type:decimal(8,4);not null;default:1.3"`
	JackpotInitialUSD   float64 `json:"jackpot_initial_usd" gorm:"type:decimal(12,2);not null;default:100"`
	JackpotIncrementUSD float64 `json:"jackpot_increment_usd" gorm:"type:decimal(12,2);not null;default:20"`
	JackpotCapUSD       float64 `json:"jackpot_cap_usd" gorm:"type:decimal(12,2);not null;default:1000"`
	CostPerUSD          float64 `json:"cost_per_usd" gorm:"type:decimal(12,6);not null;default:0.1"`
	MonthlyBudgetUSD    float64 `json:"monthly_budget_usd" gorm:"type:decimal(12,2);not null;default:0"`
	DrawnAt             int64   `json:"drawn_at" gorm:"bigint"`
	CompletedAt         int64   `json:"completed_at" gorm:"bigint"`
	CreatedAt           int64   `json:"created_at" gorm:"bigint"`
	UpdatedAt           int64   `json:"updated_at" gorm:"bigint"`
}

func (d *SubscriptionLuckyDraw) BeforeCreate(_ *gorm.DB) error {
	now := platformruntime.GetTimestamp()
	d.CreatedAt = now
	d.UpdatedAt = now
	return nil
}

func (d *SubscriptionLuckyDraw) BeforeUpdate(_ *gorm.DB) error {
	d.UpdatedAt = platformruntime.GetTimestamp()
	return nil
}

// SubscriptionLuckyReward is a per-subscription settlement snapshot for one draw.
type SubscriptionLuckyReward struct {
	Id                 int     `json:"id"`
	DrawId             int     `json:"draw_id" gorm:"index:uq_lucky_reward_draw_subscription,unique"`
	UserSubscriptionId int     `json:"user_subscription_id" gorm:"index:uq_lucky_reward_draw_subscription,unique;index"`
	UserId             int     `json:"user_id" gorm:"index"`
	LuckyNumber        string  `json:"lucky_number" gorm:"type:char(4)"`
	MembershipTier     string  `json:"membership_tier" gorm:"type:varchar(16)"`
	MatchedDigits      int     `json:"matched_digits" gorm:"type:int;not null;default:0"`
	BaseRewardUSD      float64 `json:"base_reward_usd" gorm:"type:decimal(12,2);not null;default:0"`
	TierMultiplier     float64 `json:"tier_multiplier" gorm:"type:decimal(8,4);not null;default:1"`
	JackpotRewardUSD   float64 `json:"jackpot_reward_usd" gorm:"type:decimal(12,2);not null;default:0"`
	FinalRewardQuota   int64   `json:"final_reward_quota" gorm:"type:bigint;not null;default:0"`
	CreditStatus       string  `json:"credit_status" gorm:"type:varchar(16);index"`
	CreditError        string  `json:"credit_error,omitempty" gorm:"type:varchar(512)"`
	CreditedAt         int64   `json:"credited_at" gorm:"bigint"`
	CreatedAt          int64   `json:"created_at" gorm:"bigint"`
	UpdatedAt          int64   `json:"updated_at" gorm:"bigint"`
}

func (r *SubscriptionLuckyReward) BeforeCreate(_ *gorm.DB) error {
	now := platformruntime.GetTimestamp()
	r.CreatedAt = now
	r.UpdatedAt = now
	return nil
}

func (r *SubscriptionLuckyReward) BeforeUpdate(_ *gorm.DB) error {
	r.UpdatedAt = platformruntime.GetTimestamp()
	return nil
}

// SubscriptionLuckyRewardNotification is the durable, per-reward user notice.
// A unique reward reference makes notification creation idempotent with reward settlement.
type SubscriptionLuckyRewardNotification struct {
	Id        int   `json:"id"`
	RewardId  int   `json:"reward_id" gorm:"uniqueIndex"`
	UserId    int   `json:"user_id" gorm:"index:idx_lucky_reward_notification_user_read"`
	ReadAt    int64 `json:"read_at" gorm:"index:idx_lucky_reward_notification_user_read"`
	CreatedAt int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt int64 `json:"updated_at" gorm:"bigint"`
}

func (n *SubscriptionLuckyRewardNotification) BeforeCreate(_ *gorm.DB) error {
	now := platformruntime.GetTimestamp()
	n.CreatedAt = now
	n.UpdatedAt = now
	return nil
}

func (n *SubscriptionLuckyRewardNotification) BeforeUpdate(_ *gorm.DB) error {
	n.UpdatedAt = platformruntime.GetTimestamp()
	return nil
}

// SubscriptionBlindBoxBenefitCycle records the idempotent blind-box entitlement for a subscription cycle.
type SubscriptionBlindBoxBenefitCycle struct {
	Id                 int    `json:"id"`
	UserSubscriptionId int    `json:"user_subscription_id" gorm:"index:uq_subscription_blind_box_cycle,unique"`
	BenefitCycle       string `json:"benefit_cycle" gorm:"type:varchar(128);index:uq_subscription_blind_box_cycle,unique"`
	UserId             int    `json:"user_id" gorm:"index"`
	MembershipTier     string `json:"membership_tier" gorm:"type:varchar(16)"`
	ExpectedCount      int    `json:"expected_count" gorm:"type:int;not null;default:0"`
	GrantedCount       int    `json:"granted_count" gorm:"type:int;not null;default:0"`
	Source             string `json:"source" gorm:"type:varchar(32)"`
	IdempotencyKey     string `json:"idempotency_key" gorm:"type:varchar(160);uniqueIndex"`
	StartsAt           int64  `json:"starts_at" gorm:"bigint"`
	EndsAt             int64  `json:"ends_at" gorm:"bigint"`
	Status             string `json:"status" gorm:"type:varchar(16);index"`
	CreatedAt          int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt          int64  `json:"updated_at" gorm:"bigint"`
}

func (b *SubscriptionBlindBoxBenefitCycle) BeforeCreate(_ *gorm.DB) error {
	now := platformruntime.GetTimestamp()
	b.CreatedAt = now
	b.UpdatedAt = now
	return nil
}

func (b *SubscriptionBlindBoxBenefitCycle) BeforeUpdate(_ *gorm.DB) error {
	b.UpdatedAt = platformruntime.GetTimestamp()
	return nil
}
