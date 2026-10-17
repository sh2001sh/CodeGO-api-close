package app

import (
	"encoding/json"
	"fmt"

	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	marketplacesettlement "github.com/sh2001sh/new-api/internal/marketplace/settlement"
)

func ownerIncomeBreakdown(log auditschema.Log, settlement marketplaceschema.Settlement) *OwnerIncomeBreakdown {
	type auditSnapshot struct {
		BillingSource         string   `json:"billing_source"`
		SubscriptionID        *int     `json:"subscription_id"`
		SubscriptionPlanID    *int     `json:"subscription_plan_id"`
		SubscriptionPlanTitle *string  `json:"subscription_plan_title"`
		BillingVersion        string   `json:"subscription_billing_version"`
		PolicyVersion         *string  `json:"marketplace_fee_policy_version"`
		BaseRate              *float64 `json:"marketplace_platform_commission_rate"`
		FeeRate               *float64 `json:"marketplace_transaction_fee_rate"`
		Granted               int64    `json:"credit_pack_granted_quota"`
		Paid                  int64    `json:"credit_pack_paid_quota"`
		Share                 *int64   `json:"credit_pack_channel_share_bps"`
	}
	var audit auditSnapshot
	// Invalid free-form historical audit content carries no funding evidence.
	if err := json.Unmarshal([]byte(log.Other), &audit); err != nil {
		audit = auditSnapshot{}
	}
	if settlement.ID == "" && audit.BillingSource == "" {
		return nil
	}
	detail := &OwnerIncomeBreakdown{
		SnapshotStatus: "historical_partial", BillingSource: audit.BillingSource,
		SubscriptionID: audit.SubscriptionID, SubscriptionPlanID: audit.SubscriptionPlanID,
		SubscriptionPlanTitle: audit.SubscriptionPlanTitle, AlgorithmVersion: audit.PolicyVersion,
		PackageSurchargeRate: audit.FeeRate,
	}
	if audit.BaseRate != nil {
		rate := int64(*audit.BaseRate * 10000)
		detail.BaseCommissionRateBPS = &rate
	}
	granted, paid, share := audit.Granted, audit.Paid, audit.Share
	if settlement.ID != "" {
		detail.BillingSource = settlement.BillingSource
		if settlement.SettlementGrossAmount > 0 {
			detail.SettlementGrossAmount = &settlement.SettlementGrossAmount
		}
		detail.BaseCommissionAmount = &settlement.PlatformCommission
		detail.PackageSurchargeAmount = &settlement.TransactionFee
		detail.ReferralRebateAmount = &settlement.ReferralRebateAmount
		detail.OwnerNetAmount = &settlement.OwnerNetAmount
		granted, paid = settlement.CreditPackGrantedQuota, settlement.CreditPackPaidQuota
		if granted > 0 {
			share = &settlement.CreditPackChannelShareBPS
		}
		var snapshot marketplacesettlement.IncomeSnapshot
		if len(settlement.IncomeSnapshot) > 0 {
			if err := json.Unmarshal(settlement.IncomeSnapshot, &snapshot); err == nil && snapshot.AlgorithmVersion != "" {
				detail.SnapshotStatus = "recorded"
				detail.AlgorithmVersion = &snapshot.AlgorithmVersion
				detail.BaseCommissionRateBPS = &snapshot.BaseCommissionRateBPS
				detail.PackageSurchargeRate = &snapshot.PackageSurchargeRate
				if snapshot.SubscriptionID > 0 {
					detail.SubscriptionID = &snapshot.SubscriptionID
				}
				if snapshot.SubscriptionPlanID > 0 {
					detail.SubscriptionPlanID = &snapshot.SubscriptionPlanID
				}
				if snapshot.SubscriptionPlanTitle != "" {
					detail.SubscriptionPlanTitle = &snapshot.SubscriptionPlanTitle
				}
			}
		}
		if detail.SettlementGrossAmount != nil {
			formula := fmt.Sprintf("%d - %d - %d - %d = %d", settlement.SettlementGrossAmount, settlement.PlatformCommission, settlement.TransactionFee, settlement.ReferralRebateAmount, settlement.OwnerNetAmount)
			detail.Formula = &formula
		}
	}
	if detail.BillingSource == "subscription" || detail.BillingSource == "credit_pack" {
		subscriptionType := "legacy_subscription"
		if granted > 0 || audit.BillingVersion == "credit_pack" || detail.BillingSource == "credit_pack" {
			subscriptionType = "credit_pack"
		}
		detail.SubscriptionType = &subscriptionType
	}
	if granted > 0 && paid > 0 && granted >= paid {
		bonus := float64(granted-paid) / float64(paid)
		detail.PackageBonusRate, detail.ChannelShareBPS = &bonus, share
	}
	return detail
}
