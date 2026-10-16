package billingschema

import (
	"time"

	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
)

// ReferralRebateProgram is inserted once on rollout; EffectiveAt prevents
// historical settlements from being rewarded after a restart or deployment.
type ReferralRebateProgram struct {
	ID          int   `json:"id" gorm:"column:id;primaryKey"`
	RateBPS     int64 `json:"rate_bps" gorm:"column:rate_bps;not null"`
	EffectiveAt int64 `json:"effective_at" gorm:"column:effective_at;not null"`
}

func (ReferralRebateProgram) TableName() string {
	if platformdb.UsingPostgreSQL {
		return "billing.referral_rebate_programs"
	}
	return "billing_referral_rebate_programs"
}

// One receipt per durable settlement, committed together with its wallet
// credit. Zero receipts explain excluded origins and are equally idempotent.
type ReferralConsumptionRebate struct {
	SettlementID            string    `json:"settlement_id" gorm:"column:settlement_id;primaryKey;size:64"`
	InviterID               int       `json:"inviter_id" gorm:"column:inviter_id;index;not null"`
	InviteeID               int       `json:"invitee_id" gorm:"column:invitee_id;index;not null"`
	EligibleQuota           int64     `json:"eligible_quota" gorm:"column:eligible_quota;not null"`
	RebateQuota             int64     `json:"rebate_quota" gorm:"column:rebate_quota;not null"`
	RateBPS                 int64     `json:"rate_bps" gorm:"column:rate_bps;not null"`
	ConsumedAt              int64     `json:"consumed_at" gorm:"column:consumed_at;index;not null"`
	ExclusionReason         string    `json:"exclusion_reason,omitempty" gorm:"column:exclusion_reason;size:48;not null;default:''"`
	FundingSource           string    `json:"funding_source" gorm:"column:funding_source;size:32;not null;default:''"`
	MarketplaceSettlementID string    `json:"marketplace_settlement_id,omitempty" gorm:"column:marketplace_settlement_id;size:64;not null;default:''"`
	CreatedAt               time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
}

func (ReferralConsumptionRebate) TableName() string {
	if platformdb.UsingPostgreSQL {
		return "billing.referral_consumption_rebates"
	}
	return "billing_referral_consumption_rebates"
}
