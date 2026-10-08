package incentives

import (
	"time"

	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type ReferralTerms struct {
	Eligible            bool          `json:"eligible"`
	Reason              string        `json:"reason"`
	Version             string        `json:"version"`
	PolicyRevision      int64         `json:"policy_revision"`
	RewardPPM           int64         `json:"reward_ppm"`
	ProfitSharePPM      int64         `json:"profit_share_ppm"`
	WindowDays          int           `json:"window_days"`
	DelayDays           int           `json:"delay_days"`
	MaxRewardCredits    credits.Micro `json:"max_reward_credits"`
	OwnerOnly           bool          `json:"owner_only"`
	NoRefresh           bool          `json:"no_refresh"`
	LegacyResetEligible *bool         `json:"legacy_reset_eligible,omitempty"`
}
type ReferralRewardRecord struct {
	ID                    int64         `json:"id"`
	OrderID               int64         `json:"order_id"`
	InviteeID             int64         `json:"invitee_id"`
	State                 string        `json:"state"`
	Reason                string        `json:"reason"`
	Terms                 ReferralTerms `json:"terms"`
	MaxRewardCredits      credits.Micro `json:"max_reward_credits"`
	PaidCredits           credits.Micro `json:"paid_credits"`
	RefundedRewardCredits credits.Micro `json:"refunded_reward_credits"`
	ReservedCredits       credits.Micro `json:"reserved_credits"`
	PaidAt                *time.Time    `json:"paid_at"`
	WindowUntil           *time.Time    `json:"window_until"`
}
type ReferralRewardsSummary struct {
	Records                []ReferralRewardRecord `json:"records"`
	RefundOffsetCredits    credits.Micro          `json:"refund_offset_credits"`
	OwnerOnly              bool                   `json:"owner_only"`
	NewInvitesGrantRefresh bool                   `json:"new_invites_grant_refresh"`
}
type ReferralQualificationRecord struct {
	ID                    int64         `json:"id"`
	OrderID               int64         `json:"order_id"`
	InviterID             int64         `json:"inviter_id"`
	InviteeID             int64         `json:"invitee_id"`
	State                 string        `json:"state"`
	Reason                string        `json:"reason"`
	Terms                 ReferralTerms `json:"terms"`
	RewardPPM             int64         `json:"reward_ppm"`
	ProfitSharePPM        int64         `json:"profit_share_ppm"`
	AncillaryCostPPM      int64         `json:"ancillary_cost_ppm"`
	WindowDays            int           `json:"window_days"`
	DelayDays             int           `json:"delay_days"`
	MaxRewardCredits      credits.Micro `json:"max_reward_credits"`
	PaidCredits           credits.Micro `json:"paid_credits"`
	RefundedRewardCredits credits.Micro `json:"refunded_reward_credits"`
	ReservedCredits       credits.Micro `json:"reserved_credits"`
	NetRevenueCredits     credits.Micro `json:"net_revenue_credits"`
	NetCostCredits        credits.Micro `json:"net_cost_credits"`
	PaidAt                *time.Time    `json:"paid_at"`
	WindowUntil           *time.Time    `json:"window_until"`
	CreatedAt             time.Time     `json:"created_at"`
	UpdatedAt             time.Time     `json:"updated_at"`
}
type ReferralQualificationsResult struct {
	Records  []ReferralQualificationRecord `json:"records"`
	Total    int64                         `json:"total"`
	Page     int                           `json:"page"`
	PageSize int                           `json:"page_size"`
}
