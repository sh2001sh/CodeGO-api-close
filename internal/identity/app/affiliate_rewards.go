package app

import (
	"fmt"
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
}

type AffiliateRewardsOverview struct {
	AffiliateCode             string                                             `json:"affiliate_code"`
	InvitedCount              int                                                `json:"invited_count"`
	SuccessfulPurchaseInvites int                                                `json:"successful_purchase_invites"`
	ResetOpportunity          commerceschema.SubscriptionResetOpportunitySummary `json:"reset_opportunity"`
	Invitees                  []AffiliateInviteeRewardStatus                     `json:"invitees"`
}

func GetAffiliateRewardsOverview(userID int) (*AffiliateRewardsOverview, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("invalid user id")
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
	}

	for _, invitee := range invitees {
		status := AffiliateInviteeRewardStatus{
			InviteeId:          invitee.Id,
			InviteeExternalId:  invitee.ExternalId,
			InviteeUsername:    invitee.Username,
			InviteeDisplayName: invitee.DisplayName,
			CreatedAt:          invitee.CreatedAt,
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
