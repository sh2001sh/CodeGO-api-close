package app

import (
	"fmt"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"

	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
)

type AffiliateInviteeRewardStatus struct {
	InviteeId                int    `json:"invitee_id"`
	InviteeExternalId        string `json:"invitee_external_id"`
	InviteeUsername          string `json:"invitee_username"`
	InviteeDisplayName       string `json:"invitee_display_name"`
	CreatedAt                int64  `json:"created_at"`
	MonthCardPurchased       bool   `json:"month_card_purchased"`
	ResetOpportunityEarned   bool   `json:"reset_opportunity_earned"`
	ResetOpportunityEarnedAt int64  `json:"reset_opportunity_earned_at"`
	FirstWeekConsumedQuota   int64  `json:"first_week_consumed_quota"`
	RebateQuota              int64  `json:"rebate_quota"`
	RebateEndsAt             int64  `json:"rebate_ends_at"`
}

type AffiliateRewardsOverview struct {
	AffiliateCode             string                                             `json:"affiliate_code"`
	InvitedCount              int                                                `json:"invited_count"`
	SuccessfulPurchaseInvites int                                                `json:"successful_purchase_invites"`
	ResetOpportunity          commerceschema.SubscriptionResetOpportunitySummary `json:"reset_opportunity"`
	Invitees                  []AffiliateInviteeRewardStatus                     `json:"invitees"`
	RebateRateBPS             int64                                              `json:"rebate_rate_bps"`
	RebateEffectiveAt         int64                                              `json:"rebate_effective_at"`
	FirstWeekConsumedQuota    int64                                              `json:"first_week_consumed_quota"`
	RebateTotalQuota          int64                                              `json:"rebate_total_quota"`
}

func GetAffiliateRewardsOverview(userID int) (*AffiliateRewardsOverview, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("invalid user id")
	}
	var program billingschema.ReferralRebateProgram
	if err := platformdb.DB.First(&program, 1).Error; err != nil {
		return nil, fmt.Errorf("load referral rebate program: %w", err)
	}
	type rebateAggregate struct {
		InviteeID     int
		EligibleQuota int64
		RebateQuota   int64
	}
	var rebates []rebateAggregate
	if err := platformdb.DB.Model(&billingschema.ReferralConsumptionRebate{}).
		Select("invitee_id, COALESCE(SUM(eligible_quota), 0) AS eligible_quota, COALESCE(SUM(rebate_quota), 0) AS rebate_quota").
		Where("inviter_id = ?", userID).Group("invitee_id").Scan(&rebates).Error; err != nil {
		return nil, fmt.Errorf("load referral rebates: %w", err)
	}
	rebatesByInvitee := make(map[int]rebateAggregate, len(rebates))
	var totalConsumed, totalRebate int64
	for _, rebate := range rebates {
		rebatesByInvitee[rebate.InviteeID] = rebate
		totalConsumed += rebate.EligibleQuota
		totalRebate += rebate.RebateQuota
	}

	var inviter identityschema.User
	if err := platformdb.DB.Select("id, aff_code").
		Where("id = ?", userID).
		First(&inviter).Error; err != nil {
		return nil, err
	}

	var invitees []identityschema.User
	if err := platformdb.DB.Select("id, external_id, username, display_name, created_at").
		Where("inviter_id = ?", userID).
		Order("created_at desc, id desc").
		Find(&invitees).Error; err != nil {
		return nil, err
	}

	resetOpportunity, err := commerceapp.GetUserSubscriptionResetOpportunity(userID)
	if err != nil {
		return nil, err
	}

	var ledgers []commerceschema.SubscriptionResetOpportunityLedger
	if err := platformdb.DB.
		Where("user_id = ? AND change_type = ?", userID, commerceschema.SubscriptionResetOpportunityChangeEarn).
		Order("created_at desc, id desc").
		Find(&ledgers).Error; err != nil {
		return nil, err
	}

	earnedByInvitee := make(map[int]commerceschema.SubscriptionResetOpportunityLedger, len(ledgers))
	for _, ledger := range ledgers {
		if ledger.RelatedUserId <= 0 {
			continue
		}
		earnedByInvitee[ledger.RelatedUserId] = ledger
	}

	overview := &AffiliateRewardsOverview{
		AffiliateCode:             inviter.AffCode,
		InvitedCount:              len(invitees),
		ResetOpportunity:          *resetOpportunity,
		SuccessfulPurchaseInvites: len(earnedByInvitee),
		Invitees:                  make([]AffiliateInviteeRewardStatus, 0, len(invitees)),
		RebateRateBPS:             program.RateBPS,
		RebateEffectiveAt:         program.EffectiveAt,
		FirstWeekConsumedQuota:    totalConsumed,
		RebateTotalQuota:          totalRebate,
	}

	for _, invitee := range invitees {
		status := AffiliateInviteeRewardStatus{
			InviteeId:              invitee.Id,
			InviteeExternalId:      invitee.ExternalId,
			InviteeUsername:        invitee.Username,
			InviteeDisplayName:     invitee.DisplayName,
			CreatedAt:              invitee.CreatedAt,
			FirstWeekConsumedQuota: rebatesByInvitee[invitee.Id].EligibleQuota,
			RebateQuota:            rebatesByInvitee[invitee.Id].RebateQuota,
		}
		if invitee.CreatedAt > 0 {
			status.RebateEndsAt = invitee.CreatedAt + 7*24*60*60
		}
		if ledger, ok := earnedByInvitee[invitee.Id]; ok {
			status.MonthCardPurchased = true
			status.ResetOpportunityEarned = true
			status.ResetOpportunityEarnedAt = ledger.CreatedAt
		}
		overview.Invitees = append(overview.Invitees, status)
	}

	return overview, nil
}
