package settlement

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"gorm.io/gorm"
)

var ErrUsageFundingUnconfirmed = errors.New("marketplace usage funding is not fully settled")
var ErrUsageNoIncome = errors.New("marketplace usage rounds to zero income")

// RecoverUsageTx only reconstructs an absent receipt from frozen routing and
// pricing evidence whose full debit is independently confirmed by the ledger.
// Existing released, reclaimed and forfeited receipts are never reopened.
func RecoverUsageTx(tx *gorm.DB, log auditschema.Log) (*marketplaceschema.Settlement, error) {
	if tx == nil || log.Type != auditschema.LogTypeConsume || log.RequestId == "" || log.Quota <= 0 || log.CreatedAt <= 0 {
		return nil, errors.New("invalid marketplace recovery usage")
	}
	var existing marketplaceschema.Settlement
	if err := tx.Where("request_id = ?", log.RequestId).First(&existing).Error; err == nil {
		return &existing, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	var frozen struct {
		GroupID                     string  `json:"marketplace_group_id"`
		Multiplier                  float64 `json:"marketplace_multiplier"`
		BillingSource               string  `json:"billing_source"`
		SubscriptionMultiplier      float64 `json:"subscription_multiplier"`
		SubscriptionGroupMultiplier float64 `json:"subscription_group_multiplier"`
		SubscriptionBillingVersion  string  `json:"subscription_billing_version"`
		CreditPackGrantedQuota      int64   `json:"credit_pack_granted_quota"`
		CreditPackPaidQuota         int64   `json:"credit_pack_paid_quota"`
		CreditPackChannelShareBPS   *int64  `json:"credit_pack_channel_share_bps"`
		FeePolicyVersion            string  `json:"marketplace_fee_policy_version"`
		SubscriptionID              int     `json:"subscription_id"`
		SubscriptionPlanID          int     `json:"subscription_plan_id"`
		SubscriptionPlanTitle       string  `json:"subscription_plan_title"`
	}
	if err := json.Unmarshal([]byte(log.Other), &frozen); err != nil {
		return nil, err
	}
	if frozen.GroupID == "" || (frozen.BillingSource != "wallet" && frozen.BillingSource != "subscription") {
		return nil, errors.New("marketplace recovery route or funding source is missing")
	}
	var group marketplaceschema.Group
	if err := tx.Unscoped().Where("id = ?", frozen.GroupID).First(&group).Error; err != nil {
		return nil, err
	}
	var channel marketplaceschema.Channel
	if err := tx.Unscoped().Where("id = ?", group.ChannelID).First(&channel).Error; err != nil {
		return nil, err
	}
	if group.OwnerUserID <= 0 || channel.OwnerUserID != group.OwnerUserID || channel.InternalChannelID == nil || *channel.InternalChannelID != log.ChannelId {
		return nil, errors.New("marketplace recovery channel ownership mismatch")
	}
	pack := frozen.SubscriptionBillingVersion == "credit_pack" || frozen.CreditPackGrantedQuota > 0
	if pack && (frozen.BillingSource != "subscription" || frozen.CreditPackGrantedQuota <= 0 || frozen.CreditPackPaidQuota <= 0 ||
		frozen.CreditPackPaidQuota > frozen.CreditPackGrantedQuota || frozen.CreditPackChannelShareBPS == nil ||
		*frozen.CreditPackChannelShareBPS < 0 || *frozen.CreditPackChannelShareBPS > 10000) {
		return nil, errors.New("marketplace recovery credit pack snapshot is missing")
	}
	if !pack && frozen.Multiplier <= 0 {
		return nil, errors.New("marketplace recovery price snapshot is missing")
	}
	fundingRequestID := log.RequestId
	if pack {
		digest := sha256.Sum256([]byte("credit-pack:" + log.RequestId))
		fundingRequestID = fmt.Sprintf("cp:%x", digest[:30])
	}
	var reservations []billingschema.BillingReservation
	if err := tx.Where("request_id = ?", fundingRequestID).Find(&reservations).Error; err != nil {
		return nil, err
	}
	if len(reservations) == 0 {
		return nil, ErrUsageFundingUnconfirmed
	}
	var total int64
	for _, reservation := range reservations {
		if reservation.Status != billingschema.BillingReservationStatusSettled {
			return nil, ErrUsageFundingUnconfirmed
		}
		var account billingschema.BillingAccount
		if err := tx.Where("account_id = ?", reservation.AccountID).First(&account).Error; err != nil {
			return nil, err
		}
		if frozen.BillingSource == "wallet" {
			if account.OwnerType != "user" || account.OwnerID != int64(log.UserId) || (account.AccountType != "claude_wallet" && account.AccountType != "wallet") {
				return nil, errors.New("marketplace recovery wallet owner mismatch")
			}
		} else {
			if account.AccountType != "subscription" || account.OwnerType != "user_subscription" {
				return nil, errors.New("marketplace recovery subscription account mismatch")
			}
			var subscription commerceschema.UserSubscription
			if err := tx.Unscoped().Where("id = ?", account.OwnerID).First(&subscription).Error; err != nil {
				return nil, err
			}
			if subscription.UserId != log.UserId || (subscription.CreditPackGrantedQuota > 0) != pack {
				return nil, errors.New("marketplace recovery subscription owner mismatch")
			}
			if pack && (subscription.CreditPackGrantedQuota != frozen.CreditPackGrantedQuota || subscription.CreditPackPaidQuota != frozen.CreditPackPaidQuota || subscription.CreditPackChannelShareBPS != *frozen.CreditPackChannelShareBPS) {
				return nil, errors.New("marketplace recovery purchase snapshot mismatch")
			}
		}
		var native billingschema.BillingSettlement
		if err := tx.Where("reservation_id = ? AND status = ?", reservation.ReservationID, billingschema.BillingSettlementStatusCompleted).First(&native).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrUsageFundingUnconfirmed
			}
			return nil, err
		}
		if native.ActualAmount < 0 || native.ActualAmount > int64(log.Quota)-total {
			return nil, errors.New("marketplace recovery native debit exceeds usage")
		}
		total += native.ActualAmount
	}
	if total != int64(log.Quota) {
		return nil, ErrUsageFundingUnconfirmed
	}
	gross := total
	if frozen.BillingSource == "subscription" && !pack {
		gross = total / 10
		if total%10 >= 5 {
			gross++
		}
	}
	if gross <= 0 {
		return nil, ErrUsageNoIncome
	}
	share := int64(0)
	if frozen.SubscriptionMultiplier == 0 {
		frozen.SubscriptionMultiplier = frozen.SubscriptionGroupMultiplier
	}
	if frozen.CreditPackChannelShareBPS != nil {
		share = *frozen.CreditPackChannelShareBPS
	}
	if err := RecordTx(tx, RecordParams{
		RequestID: log.RequestId, GroupID: group.ID, OwnerUserID: group.OwnerUserID, ConsumerUserID: log.UserId,
		BillingSource: frozen.BillingSource, ConsumerDebitAmount: total, SettlementGrossAmount: gross,
		WalletMultiplier: frozen.Multiplier, SubscriptionMultiplier: frozen.SubscriptionMultiplier,
		CreditPackGrantedQuota: frozen.CreditPackGrantedQuota, CreditPackPaidQuota: frozen.CreditPackPaidQuota,
		CreditPackChannelShareBPS: share, FeePolicyVersion: frozen.FeePolicyVersion,
		SubscriptionID: frozen.SubscriptionID, SubscriptionPlanID: frozen.SubscriptionPlanID, SubscriptionPlanTitle: frozen.SubscriptionPlanTitle,
	}); err != nil {
		return nil, err
	}
	if err := tx.Where("request_id = ?", log.RequestId).First(&existing).Error; err != nil {
		return nil, err
	}
	// Keep the original maturity date; recovery must not restart the 24h hold.
	createdAt := time.Unix(log.CreatedAt, 0).UTC()
	if existing.Status == statusPending {
		if err := tx.Model(&existing).Updates(map[string]any{"created_at": createdAt, "available_at": createdAt.Add(24 * time.Hour)}).Error; err != nil {
			return nil, err
		}
		existing.CreatedAt, existing.AvailableAt = createdAt, createdAt.Add(24*time.Hour)
	}
	return &existing, nil
}
